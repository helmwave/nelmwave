# nelmwave — Development Spec & Agent Prompt

> **Назначение документа.** Это исчерпывающее техническое задание-промпт для AI-агента,
> который будет разрабатывать `nelmwave` с нуля. Читай целиком перед началом. Все внешние
> API (nelm, gomplate, confijer, fileref) могут меняться по версиям — **сверяйся с исходниками
> и pkg.go.dev, не полагайся слепо на сигнатуры из этого документа**. Решения, помеченные как
> **[FIXED]**, приняты владельцем и менять их нельзя без явного согласования.

---

## 0. Роль агента

Ты — senior Go-инженер. Твоя задача — спроектировать и реализовать `nelmwave`: декларативный
оркестратор релизов поверх **nelm** (замена Helm от команды werf), идейный аналог
[`helmwave`](https://github.com/helmwave/helmwave), но с новой схемой и новым движком.

Работай итеративно по milestone'ам (раздел 18). После каждого milestone — компилируемый,
покрытый тестами код. Не заглушки ради заглушек: каждый слой должен работать end-to-end к концу
своего milestone.

---

## 1. Vision

`nelmwave` управляет множеством релизов из одного декларативного манифеста `nelmwave.yml`:
рендерит его через шаблонизатор, резолвит values и сопутствующие файлы из произвольных
источников, строит граф зависимостей между релизами и ресурсами, и применяет всё это через
nelm — параллельно, с учётом порядка.

Аналогия с helmwave по ролям, но **не** по формату:
- `helmwave.yml.tpl` → **`nelmwave.yml.tpl`** (шаблон)
- `helmwave.yml` (planfile) → **`.nelmwave/`** (собранный план + артефакты)
- `helm` (SDK) → **`nelm`** (SDK)
- `tags` → **`labels`** (k8s-селекторы)
- `depends_on` → **`needs`** (между релизами И между ресурсами)

---

## 2. Технологический стек — зафиксированные решения [FIXED]

| Что | Решение |
|---|---|
| Язык | **Go 1.26+** |
| Бинарь / module | бинарь `nelmwave`, module path `github.com/helmwave/nelmwave` |
| Движок деплоя | **nelm как Go-библиотека** (`github.com/werf/nelm/pkg/action`), НЕ CLI-обёртка |
| Шаблонизатор | **gomplate v5 как Go-библиотека**, делимитеры по умолчанию **`[[ ]]`** |
| Логгер | **[uber-go/zap](https://github.com/uber/zap)** |
| Конфиг-загрузчик | **[helmwave/confijer](https://github.com/helmwave/confijer)** (defaults по Go-типам через reflection) |
| Datasources (values, store files) | **[helmwave/fileref](https://github.com/helmwave/fileref)** поверх gomplate DataSources |
| Схема nelmwave.yml | **новая**, вдохновлённая helmwave (НЕ drop-in совместимость) |
| Модель needs | **DAG с параллельностью** (топосорт + concurrent-исполнение независимых веток) |
| Селектор | **Kubernetes-style label selector** (`app=api,env in (prod,stg),tier!=db`) |
| Хранилище build | каталог **`.nelmwave/`** (planfile + распакованные charts/values/store-files) |
| Универсальный chart | **вшит в бинарь** через `go:embed`, конфигурируется через confijer-схему |
| CLI | **cobra** (рекомендуется; согласуй, если хочешь иное) |

**MVP команды (первый релиз):** `build`, `up`, `down`, `diff` (aka `plan`).
`status` / `list` — следующая итерация, но заложи место в архитектуре.

---

## 3. Ключевые зависимости и их роль

### 3.1 nelm (`github.com/werf/nelm/pkg/action`)
Движок. Используем как библиотеку. Актуальные (на момент написания) функции — **проверь версии**:

- `ReleaseInstall(ctx, releaseName, releaseNamespace, ReleaseInstallOptions) error` — деплой/апгрейд.
- `ReleaseUninstall(ctx, releaseName, releaseNamespace, ReleaseUninstallOptions) error` — удаление.
- `ReleasePlanInstall(ctx, releaseName, releaseNamespace, ReleasePlanInstallOptions) error` — plan/diff
  (есть `ErrorIfChangesPlanned`, `PlanArtifactPath`).
- `ChartRender(ctx, ChartRenderOptions) (*ChartRenderResultV2, error)` — рендер манифестов.
- `ReleaseList(...)`, `ReleaseGet(...)` — для будущих `list`/`status`.

nelm умеет:
- OCI и classic helm-repo charts;
- **порядок применения ресурсов через аннотации** (deploy dependencies / weight) — это фундамент
  для «needs между ресурсами» (см. §11.2). **Уточни точные имена аннотаций в исходниках nelm**
  (семейство `werf.io/deploy-dependency-*`, `werf.io/weight` и внешние зависимости).

### 3.2 gomplate v5 (Go-library)
Рендер `nelmwave.yml.tpl` и любых `*.tpl`-datasource'ов.
- Делимитеры по умолчанию **`[[` / `]]`** [FIXED] — задай через опции LeftDelim/RightDelim.
- Прокинь стандартный контекст: env, а также кастомные функции/переменные nelmwave
  (например `.Release`, окружение, версия). Список кастомных данных — согласуй в §5.3.
- gomplate — тот же движок, что даёт DataSources, которые использует fileref.

### 3.3 confijer (`github.com/helmwave/confijer`)
Загрузка **уже отрендеренного** `nelmwave.yml` в Go-структуры с дефолтами по типам.
API: `UnmarshalYAML(data, out)` / `UnmarshalYAMLFile(path, out)`.
Использование:
1. Парсинг `nelmwave.yml` → `config.Config`.
2. Значения универсального chart'а (§12): top-level ключ-тип задаёт дефолты для всех релизов,
   каждый релиз переопределяет точечно.

Приоритет значений (низший→высший): zero → тег `default:"..."` → рекурсивные дефолты типа →
явные значения по точному пути. Проектируй структуры конфигов с оглядкой на эту модель.

### 3.4 fileref (`github.com/helmwave/fileref`)
Адаптер gomplate DataSources: «читает URL scheme и отдаёт контент». Движок для **Values** и
**StoreFiles**. Поведение по расширению источника:
- `*.yml` / `*.yaml` — copy как есть;
- `*.yml.tpl` — рендер как шаблон (gomplate) перед использованием;
- `*.yml.sops` — расшифровка SOPS перед использованием.

Пользователь указывает источник в поле `src` как URL с любой схемой gomplate
(`file://`, `env:`, `vault://`, `aws+sm://`, `aws+smp://`, `s3://`, `http(s)://`, `git://`,
`consul://`, …). **Поддержать все датасорсы, которые даёт gomplate/fileref** [FIXED] — не хардкодить
список, а делегировать резолв в fileref.

### 3.5 zap
Логирование во всём приложении.
- Дефолтный формат: **`console`** (цветной, dev) с **авто-переключением на `json`**, когда вывод
  не в TTY / в CI. Флаги: `--log-level` (debug|info|warn|error, default info),
  `--log-format` (auto|console|json, default auto). [FIXED]
- Единый `*zap.Logger`/`SugaredLogger`, прокидывается через context или DI. Никаких `fmt.Println`
  в бизнес-логике.

---

## 4. Архитектура

Предлагаемая структура пакетов (уточни при необходимости, но держи слои раздельными):

```
github.com/helmwave/nelmwave
├── cmd/nelmwave/           # main(): сборка CLI
├── internal/
│   ├── cli/                # cobra-команды: build, up, down, diff
│   ├── config/             # схема nelmwave.yml, загрузка через confijer + gomplate
│   │   ├── config.go       # корневой Config
│   │   ├── release.go      # Release
│   │   ├── repository.go   # Repository (helm repo)
│   │   ├── registry.go     # Registry (OCI)
│   │   └── selector.go     # k8s label selector parsing/matching
│   ├── tpl/                # gomplate-рендер (.tpl → bytes), делимитеры [[ ]], контекст
│   ├── datasource/         # обёртка над fileref для values и store-files
│   ├── plan/               # planfile: сборка, сериализация в .nelmwave/, чтение
│   ├── graph/              # DAG: needs между релизами, топосорт, параллельный проход
│   ├── release/            # адаптер к nelm/pkg/action (install/uninstall/plan/render)
│   ├── chart/              # универсальный встроенный chart (go:embed) + confijer-рендер values
│   ├── kubedep/            # проставление аннотаций nelm для needs между ресурсами
│   ├── log/                # zap setup, флаги, auto console/json
│   └── version/            # версия бинаря
├── pkg/                    # если нужно публичное API — по минимуму
└── testdata/
```

**Принципы:**
- `internal/config` не знает про nelm; `internal/release` не знает про CLI. Слои вниз.
- Всё, что может ходить по сети/ФС, принимает `context.Context` (для `up`/`down` — отмена по Ctrl-C).
- Ошибки — обёрнутые (`fmt.Errorf("...: %w", err)`), с контекстом «какой релиз/файл».

---

## 5. Формат `nelmwave.yml` (+ `.tpl`)

### 5.1 Пример целевой схемы (ориентир, финализируй в коде)

```yaml
# nelmwave.yml.tpl  — рендерится gomplate (делимитеры [[ ]])
project: my-platform

registries:
  - host: registry.example.com
    username: [[ .Env.REGISTRY_USER ]]
    password: [[ .Env.REGISTRY_PASS ]]

repositories:
  - name: bitnami
    url: https://charts.bitnami.com/bitnami
    # переопределение helm repo config / смежных настроек — см. §13
    force_update: true

releases:
  - name: postgres
    namespace: data
    labels:
      app: postgres
      tier: db
      env: prod
    chart:
      ref: bitnami/postgresql        # helm repo chart
      version: 15.x
    values:
      - src: file://values/pg.yml.tpl        # fileref: render перед мёрджем
      - src: vault://secret/pg#password      # любой datasource
        # опционально: strict / optional

  - name: api
    namespace: app
    labels:
      app: api
      tier: backend
      env: prod
    needs:
      - postgres                      # needs МЕЖДУ релизами (DAG)
    chart:
      ref: oci://registry.example.com/charts/api    # OCI
      version: 1.4.2
    values:
      - src: file://values/api.yml.tpl
    store:
      - src: file://extra/netpol.yml          # StoreFiles (fileref) → .nelmwave/
        dst: manifests/netpol.yml

  - name: cache
    namespace: app
    labels: { app: redis, tier: cache, env: prod }
    # без chart.ref → используется ВСТРОЕННЫЙ универсальный chart (§12)
    universal:
      image: redis:7
      service:
        port: 6379
      # confijer-схема универсального chart'а
```

### 5.2 Структуры (confijer-friendly)
- Корень `Config`: `Project string`, `Registries []Registry`, `Repositories []Repository`,
  `Releases []Release`, глобальные `Values []ValueRef`.
- `Release`: `Name`, `Namespace`, `Labels map[string]string`, `Needs []string`,
  `Chart` (`Ref`, `Version`), `Values []ValueRef`, `Store []StoreRef`, `Universal *UniversalValues`,
  плюс проброс нужных nelm-опций (timeout, atomic/auto-rollback, wait, create-namespace и т.п.).
- `ValueRef` / `StoreRef`: `Src string` (URL для fileref), `Dst string` (для store), флаги
  `Optional`, `Strict`, `RenderInline` (если нужно принудительно рендерить).
- Проектируй теги `default:"..."` там, где confijer должен подставлять дефолты.

### 5.3 Контекст gomplate для `nelmwave.yml.tpl`
Согласуй минимальный набор: `.Env` (окружение), `.Project`, служебные функции gomplate
(`datasource`, `file`, `env`, `strings`, …). Реши, нужен ли доступ к per-release данным на этапе
рендера корневого файла (обычно нет — релизы разворачиваются уже из структуры).

---

## 6. Пайплайн `build` → `.nelmwave/`

`nelmwave build`:
1. Найти `nelmwave.yml.tpl` (или `nelmwave.yml`, если без шаблона). Рендер через gomplate (`[[ ]]`).
2. Загрузить результат в `config.Config` через confijer.
3. Валидация: уникальность имён релизов, существование целей `needs`, отсутствие циклов в DAG,
   корректность label-ключей/значений, наличие chart.ref ИЛИ universal.
4. Для каждого релиза: резолв **values** и **store files** через fileref (render/copy/sops),
   deep-merge values (§9).
5. (Опц., но желательно) распаковать/подготовить charts локально для воспроизводимости в CI.
6. Записать **planfile** и артефакты в `.nelmwave/`:
   ```
   .nelmwave/
   ├── planfile.yml            # плоский, полностью зарезолвленный план (все релизы, values inline/ссылки, needs, labels)
   ├── values/<release>.yml    # смёрдженные values
   ├── store/<release>/...     # store files
   └── charts/<release>/...    # (если распаковываем)
   ```
   planfile должен быть **самодостаточным**: `up`/`down`/`diff` читают только `.nelmwave/`,
   заново не рендерят шаблоны (детерминизм в CI). [FIXED: каталог `.nelmwave/`]

---

## 7. `up` / `down` / `diff` — семантика

Все три:
1. Читают `.nelmwave/planfile.yml` (если его нет — предложить сначала `build`, либо флаг
   `--build` для авто-сборки).
2. Применяют **селекцию по labels** (§8) → подмножество релизов.
3. Строят DAG по `needs` внутри выбранного подмножества (§10), проверяют, что needs не ведут
   в невыбранные релизы (или тянут их — согласуй политику; по умолчанию: **ошибка**, если нужный
   релиз отфильтрован; флаг `--include-needs` чтобы дотягивать).

- **`up`**: топосорт → параллельный деплой независимых веток через `action.ReleaseInstall`.
  Concurrency-лимит флагом `--concurrency` (default = число «корней» или разумный предел).
- **`diff`/`plan`**: `action.ReleasePlanInstall` по каждому релизу (можно параллельно, needs здесь
  для порядка не критичны, но соблюдай для корректного diff зависимых). Ненулевой exit code при
  `--detailed-exitcode`, если есть изменения (`ErrorIfChangesPlanned`).
- **`down`**: `action.ReleaseUninstall` в **обратном** топологическом порядке (сначала зависящие,
  потом зависимости).

Общие флаги: `--concurrency`, `-l/--selector`, `--namespace` (override), `--kube-context`,
`--timeout`, `--dry-run` (для up → делегировать в plan).

---

## 8. Labels & селекция [FIXED: k8s-style]

- В `nelmwave.yml` у каждого релиза `labels: map[string]string`.
- CLI: `-l/--selector 'app=api,env in (prod,stg),tier!=db'`.
- **Используй `k8s.io/apimachinery/pkg/labels`** (`labels.Parse`, `Selector.Matches(labels.Set)`) —
  не пиши свой парсер. Поддержи `=`, `==`, `!=`, `in`, `notin`, `exists`, `!key`.
- Пустой селектор = все релизы.
- Возможность нескольких `-l` (объединять как AND — согласуй) — по желанию.

---

## 9. Values (fileref/gomplate datasources)

- Каждый `ValueRef.Src` — URL, резолвится через **fileref** (поддержать **все** datasource'ы).
- По расширению источника: `.yml`→copy, `.yml.tpl`→gomplate-render, `.yml.sops`→SOPS-decrypt.
- **Порядок мёрджа** [FIXED]: глобальные `Values` (корень config) → per-release `Values`,
  **deep-merge**, последний источник побеждает (override). Скаляры/массивы — replace, мапы — merge.
- Результат — `.nelmwave/values/<release>.yml`, передаётся в nelm как values релиза.
- Флаги источника: `optional` (нет файла → пропустить, не падать), `strict`.

---

## 10. StoreFiles (fileref)

- `Release.Store []StoreRef{ Src, Dst, ... }` — произвольные файлы, резолвятся через fileref
  (те же правила copy/render/sops) и складываются в `.nelmwave/store/<release>/<Dst>`.
- Назначение: доп. манифесты и сопутствующие артефакты релиза (напр. NetworkPolicy, CRD, конфиги),
  которые нужно приложить/сохранить рядом с планом. Реши политику применения:
  - как дополнительные манифесты, подаваемые nelm вместе с релизом, **или**
  - просто складируются как артефакты (для аудита/дальнейших шагов).
  Согласуй — по умолчанию: **сохранять как артефакты**, применение доп. манифестов — отдельная опция.

---

## 11. Needs (зависимости)

### 11.1 Между релизами → DAG [FIXED]
- `Release.Needs []string` — имена релизов, которые должны быть применены раньше.
- Построй ориентированный граф, проверь ацикличность (иначе ошибка build), топосорт.
- `up`: параллельно исполняй независимые вершины, соблюдая рёбра (errgroup + семафор + ожидание
  завершения предков). `down`: обратный порядок.

### 11.2 Между ресурсами → аннотации nelm
- nelmwave должен уметь выражать порядок применения **ресурсов внутри релиза** и (возможно)
  межрелизные ресурсные зависимости, транслируя это в **аннотации nelm** (deploy-dependencies /
  weight / external dependencies).
- **Изучи точный контракт аннотаций nelm** (семейство `werf.io/*`) и предоставь способ их задавать:
  - для встроенного универсального chart'а — генерировать аннотации из декларации;
  - для внешних chart'ов — прокидывать/патчить (реши: через post-render hook nelm, если есть, или
    через values, если chart их поддерживает).
- Это отдельный слой `internal/kubedep`. Если nelm-API не позволяет патчить произвольные чужие
  манифесты — задокументируй ограничение и ограничься универсальным chart'ом на первом этапе.

---

## 12. Встроенный универсальный chart [FIXED]

- Обычный Helm-chart, **вшитый в бинарь** через `go:embed` (каталог `internal/chart/universal/`).
- Активируется, когда у релиза **не задан** `chart.ref`, но задан блок `universal:`.
- Values универсального chart'а описываются **confijer-схемой**: top-level тип задаёт дефолты для
  всех релизов, каждый релиз переопределяет точечно (используй модель приоритетов confijer).
- **Охват ресурсов на старте** (реализуй именно этот набор, расширяемо):
  `Deployment`, `Service`, `Ingress`, `ConfigMap`, `Secret`, `HPA`. Заложи расширение
  (`ServiceAccount`, `PVC`, `CronJob`, `NetworkPolicy` — потом).
- Chart подаётся в `action.ReleaseInstall` как локальный путь (распакованный embed во временный
  каталог или `.nelmwave/charts/<release>/`).
- Поддержи аннотации needs-между-ресурсами (§11.2) прямо в шаблонах chart'а.

---

## 13. Repositories / OCI Registries [FIXED]

- **OCI registry**: `registries: [{host, username, password}]`. Логин/креды прокинуть в nelm
  (через его registry-client опции). Поддержи анонимный доступ.
- **Helm repositories**: `repositories: [{name, url, username, password, ...}]`. Добавление/обновление
  repo, резолв `repo/chart`.
- **Переопределение helm repo config и смежных настроек** [FIXED]: дай возможность указать/переопределить
  путь к `repositories.yaml` / cache / registry config (аналог `--repository-config`,
  `--repository-cache`, `--registry-config`), а также per-repo флаги (`force_update`,
  `insecure_skip_tls_verify`, `pass_credentials`, ca-file и т.п.). Изучи, какие из этих настроек
  nelm принимает через свой API, и прокинь их; недостающее — реши через генерацию временного
  repo-config для nelm.

---

## 14. Logger (zap) [FIXED]

- Инициализация в `internal/log`: уровень (`--log-level`), формат (`--log-format auto|console|json`),
  auto = console в TTY, json иначе.
- Структурные поля: `release`, `namespace`, `phase` (build/up/down/diff), `src` (для datasource).
- Прогресс параллельного деплоя — читаемо (кто стартовал/завершился/упал).

---

## 15. CLI

`cobra`, корневая команда `nelmwave`. Команды MVP:

- `nelmwave build [--file nelmwave.yml.tpl] [--output .nelmwave]`
- `nelmwave up   [-l selector] [--concurrency N] [--build] [--include-needs] [--dry-run]`
- `nelmwave down [-l selector] [--concurrency N]`
- `nelmwave diff [-l selector] [--detailed-exitcode]`  (алиас `plan`)
- Глобальные: `--log-level`, `--log-format`, `--kube-context`, `--kube-config`, `--version`.

Заложи (не реализуй в MVP): `status`, `list`.

---

## 16. Конфигурация самого nelmwave через confijer

- Помимо `nelmwave.yml`, поддержи опциональный конфиг инструмента (дефолты concurrency, пути,
  формат логов), загружаемый через confijer. Приоритет: флаги CLI > env > файл конфига > дефолты.
- Не усложняй в MVP: минимально — дефолты, которые удобно держать в одном месте.

---

## 17. Конкурентность, отмена, ошибки

- `context.Context` от корня CLI, отмена по SIGINT/SIGTERM → graceful stop (не бросать половину
  DAG в неизвестном состоянии; текущие релизы дать доработать или отменить по политике nelm).
- Параллелизм: `golang.org/x/sync/errgroup` + семафор; лимит `--concurrency`.
- Ошибка одного релиза: по умолчанию **fail-fast** для его ветки DAG, но независимые ветки
  продолжаются; в конце — агрегированный отчёт. Флаг `--fail-fast/--no-fail-fast` для выбора.

---

## 18. Milestones (порядок работ)

1. **M1 — Скелет + конфиг.** `go.mod` (Go 1.26), cobra-скелет, zap, `internal/config` со схемой,
   confijer-загрузка, gomplate-рендер `nelmwave.yml.tpl` (`[[ ]]`). Команда `build` рендерит и
   валидирует, пишет `planfile.yml` в `.nelmwave/`. Тесты на парсинг/валидацию/рендер.
2. **M2 — Datasources.** Интеграция fileref: values (deep-merge, порядок) + store files. Артефакты
   в `.nelmwave/`. Тесты с `file://`, `env:` (+ mock хотя бы одного секрет-стора).
3. **M3 — DAG + nelm up/down.** `internal/graph` (топосорт, циклы), адаптер `internal/release`
   поверх `action.ReleaseInstall/ReleaseUninstall`, параллельное исполнение, `up`/`down`.
   Селекция по labels (`-l`). Интеграционный тест на kind/локальный кластер (или мок nelm-слоя).
4. **M4 — diff/plan.** `action.ReleasePlanInstall`, `--detailed-exitcode`.
5. **M5 — Универсальный chart.** `go:embed` chart, confijer-values, набор ресурсов из §12,
   активация через `universal:`.
6. **M6 — Registries/repos.** OCI login, helm repo, переопределение repo/registry config.
7. **M7 — Needs между ресурсами.** Аннотации nelm в универсальном chart'е (+ по возможности внешние).
8. **M8 — Полировка.** Логи, ошибки, docs, `--help`, примеры в `examples/`.

Каждый milestone: компилируется, `go vet` + `golangci-lint` чисто, есть тесты, есть краткий
раздел в README.

---

## 19. Definition of Done

- `go build ./...` и `go test ./...` зелёные; линтер чист.
- `nelmwave build` из примера `examples/` даёт корректный `.nelmwave/planfile.yml`.
- `nelmwave up -l '...'` разворачивает подмножество релизов в правильном порядке (проверено на
  kind), `down` сносит в обратном; `diff` показывает изменения.
- Универсальный chart разворачивает Deployment+Service+Ingress+ConfigMap+Secret+HPA из `universal:`.
- Values и store-files тянутся из ≥2 разных datasource-схем.
- README с быстрым стартом и описанием схемы `nelmwave.yml`.

---

## 20. Открытые вопросы для уточнения у владельца

Отметь и задай, если всплывёт по ходу (не блокируйся — предложи дефолт и продолжай):
1. Политика при `needs` в отфильтрованный релиз: ошибка vs авто-подтягивание (`--include-needs`)?
   *(дефолт: ошибка)*
2. StoreFiles: только артефакты vs авто-применение доп. манифестов? *(дефолт: артефакты)*
3. Множественные `-l`: AND vs отдельные группы?
4. Точные nelm-опции для repo/registry override и аннотаций ресурсных зависимостей — что реально
   принимает API текущей версии nelm.
5. Нужен ли SOPS-encrypt/decrypt как отдельные команды (nelm умеет secrets) или только пассивная
   расшифровка через fileref.

---

## 21. Жёсткие правила

- **Не** делай drop-in совместимость с `helmwave.yml` — схема новая.
- **Не** shell-out'и в бинарь nelm/gomplate — только Go-библиотеки.
- Делимитеры шаблонов — **`[[ ]]`** по умолчанию.
- Селекторы — **k8s-style**, парсинг через `apimachinery/labels`.
- Артефакты сборки — только в **`.nelmwave/`**; runtime-команды не перерендеривают шаблоны.
- Всё логируй через **zap**; весь I/O — через **context**.
- Внешние API (nelm/gomplate/confijer/fileref) — **сверяй с исходниками**, версии фиксируй в `go.mod`.
