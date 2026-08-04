# nelmwave — Development Spec & Agent Prompt

> **Назначение документа.** Это исчерпывающее техническое задание-промпт для AI-агента,
> который будет разрабатывать `nelmwave` с нуля. Читай целиком перед началом. Все внешние
> API (nelm, gomplate, confijer, fileref) могут меняться по версиям — **сверяйся с исходниками
> и pkg.go.dev, не полагайся слепо на сигнатуры из этого документа**. Решения, помеченные как
> **[FIXED]**, приняты владельцем и менять их нельзя без явного согласования.

> **Уточнения, принятые в ходе разработки** (владелец, 2026-08-04). Имеют приоритет над
> исходными формулировками ниже, где расходятся:
> - **Коллекции — мапы, не списки.** `repositories` (ключ = alias/host; helm-repo `https://` и OCI
>   `oci://` вместе, отличаются по схеме URL; значение — голая строка-URL или объект),
>   `releases` (ключ = **uniqname** `name[@namespace[@kubecontext]]`). ns/ctx опциональны — если
>   опущены, берётся текущий kube-context/его дефолтный namespace (резолв на apply-этапе).
>   Value-структуры не содержат поля-идентификатора. kube-context может содержать `@`.
> - **`chart.name`** вместо `chart.ref`.
> - **`values` и `store` — единый тип `FileRef`** (`Src, Dst, Optional, Strict`), принимает 4
>   эквивалентные формы (строка/мапа × со схемой/без; нет схемы или `file://` → голый путь).
> - **`needs` — структура**: `needs.releases` (мапа по uniqname → `{strict, …}`) + инлайн k8s-селектор
>   `needs.matchLabels` + `needs.matchLabelsExpressions`. Зависимость — объединение.
> - **Datasources — свой резолвер** на gomplate v5 (fileref v0.2.0 непригоден как библиотека).
> - **SOPS отложен**: в MVP только `.yml`/`.yml.tpl`; `.yml.sops` → «not supported yet».
> - **Встроенный универсальный chart отложен** (пост-MVP): `chart.name` обязателен, блока
>   `universal:` в схеме пока нет (§12/M5 — на будущее).
> - **confijer биндит по `json`-тегу** (не `yaml`) через `EqualFold`; в config-структурах — двойные
>   теги `json`+`yaml` (json для загрузки, yaml для сериализации planfile).

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
| Datasources (values, store files) | **собственный резолвер `internal/datasource` поверх gomplate v5** (fileref v0.2.0 непригоден как библиотека — `package main`, gomplate v4; см. §3.4) |
| Схема nelmwave.yml | **новая**, вдохновлённая helmwave (НЕ drop-in совместимость). Коллекции — **мапы по идентификатору**, релиз — **uniqname** `name[@namespace[@kubecontext]]` (см. §5) |
| Модель needs | **DAG с параллельностью** (топосорт + concurrent-исполнение независимых веток). Зависимости — по uniqname (`needs.releases`) и по инлайн k8s-селектору (`needs.matchLabels`/`needs.matchLabelsExpressions`) |
| Селектор | **Kubernetes-style label selector** (`app=api,env in (prod,stg),tier!=db`) |
| Хранилище build | каталог **`.nelmwave/`** (planfile + распакованные charts/values/store-files) |
| Универсальный chart | **отложен (пост-MVP)**; в перспективе — вшит через `go:embed`, конфиг по confijer-схеме |
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

### 3.4 datasource-резолвер (`internal/datasource`, поверх gomplate v5)
> **Изменено:** fileref v0.2.0 непригоден как библиотека (корень — `package main`, логика в
> `internal/`, ничего не возвращает как `[]byte`, тянет gomplate **v4**, SOPS-ветка сломана).
> Поэтому вместо fileref — **собственный резолвер поверх gomplate v5**, сохраняющий тот же контракт.

Движок для **Values** и **StoreFiles**: «читает URL scheme и отдаёт контент как `[]byte`».
Поведение по расширению источника:
- `*.yml` / `*.yaml` — copy как есть;
- `*.yml.tpl` — рендер как шаблон (gomplate, делимитеры `[[ ]]`) перед использованием;
- `*.yml.sops` — **отложено** (в MVP → ошибка «not supported yet»; см. §20 Q5).

Пользователь указывает источник в поле `src` как URL с любой схемой gomplate
(`file://`, `env:`, `vault://`, `aws+sm://`, `aws+smp://`, `s3://`, `http(s)://`, `git://`,
`consul://`, …), либо голым путём (без схемы = локальный файл). **Поддержать все датасорсы,
которые даёт gomplate v5** [FIXED] — не хардкодить список, а делегировать резолв в gomplate.
Форма `src` нормализуется на этапе parse: 4 эквивалентных написания, `file://`↔голый путь (см. §9).

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
│   │   ├── config.go       # корневой Config (мапы по идентификатору)
│   │   ├── release.go      # Release + Chart{Name,Version}
│   │   ├── uniqname.go     # Uniqname name[@ns[@ctx]]: parse/канонизация ключей
│   │   ├── needs.go        # Needs{Releases, MatchLabels, MatchLabelsExpressions}, DirectNeeds
│   │   ├── fileref.go      # FileRef (единый для values и store)
│   │   ├── repository.go   # Repository (helm-repo + OCI, объединены; IsOCI)
│   │   ├── load.go         # Parse: gomplate-нормализация + confijer + канонизация
│   │   ├── validate.go     # валидация + детект циклов (вкл. label-рёбра)
│   │   └── selector.go     # k8s label selector parsing/matching
│   ├── tpl/                # gomplate-рендер (.tpl → bytes), делимитеры [[ ]], контекст
│   ├── datasource/         # свой резолвер поверх gomplate v5 (Resolve + MergeValues)
│   ├── build/              # оркестрация: резолв datasources → артефакты .nelmwave/
│   ├── graph/              # параллельный DAG-исполнитель (Run, Reverse)
│   ├── release/            # Applier (интерфейс) + NelmApplier поверх nelm/pkg/action
│   ├── repo/               # резолв chart.name→nelm (helm-repo/OCI), docker config для OCI
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

Коллекции — **мапы по идентификатору**: `repositories` (ключ = alias/host; helm-repo и OCI вместе),
`releases` (ключ = **uniqname** `name[@namespace[@kubecontext]]`). ns/ctx в ключе опциональны.

```yaml
# nelmwave.yml.tpl  — рендерится gomplate (делимитеры [[ ]])
project: my-platform

# repositories: helm-repo (https://) и OCI (oci://) вместе. Значение — голая
# строка-URL или объект (когда нужны креды/флаги).
repositories:
  bitnami: https://charts.bitnami.com/bitnami   # голый URL (короткая форма)
  registry.example.com:                          # полная форма: OCI + креды
    url: oci://registry.example.com
    username: [[ .Env.REGISTRY_USER ]]
    password: [[ .Env.REGISTRY_PASS ]]
    # переопределение helm repo config / смежных настроек — см. §13

releases:
  postgres@data:                        # ключ = uniqname name@namespace[@kubecontext]
    labels:
      app: postgres
      tier: db
      env: prod
    chart:
      name: bitnami/postgresql          # helm repo chart (было chart.ref)
      version: 15.x
    values:
      - values/pg.yml.tpl               # FileRef: любая из 4 форм (см. §9)
      - src: vault://secret/pg#password # любой datasource; можно strict/optional

  api@app:
    labels:
      app: api
      tier: backend
      env: prod
    needs:                             # структура: releases и/или labels (см. §11)
      releases:
        postgres@data:
          strict: true
    chart:
      name: oci://registry.example.com/charts/api    # OCI
      version: 1.4.2
    values:
      - src: values/api.yml.tpl
    store:
      - src: extra/netpol.yml          # StoreFiles → .nelmwave/
        dst: manifests/netpol.yml

  cache@app:
    labels: { app: redis, tier: cache, env: prod }
    needs:
      matchLabels: { tier: db }        # инлайн k8s-селектор: ждать все релизы-БД
      # matchLabelsExpressions: [ { key: env, operator: In, values: [prod, stg] } ]
    chart:
      name: bitnami/redis
      version: 20.x
    # (встроенный универсальный chart и блок universal: отложены — §12)
```

### 5.2 Структуры (confijer-friendly) — реализовано
- Корень `Config`: `Project string`, `Repositories map[string]Repository`
  (ключ=alias/host; helm-repo + OCI, `IsOCI()` по схеме; значение — голый URL или объект),
  `Releases map[string]Release` (ключ=uniqname), глобальные `Values []FileRef`.
- Идентичность релиза — тип **`Uniqname{Name, Namespace, KubeContext}`** (парсинг/канонизация
  ключа и `needs.releases`). Поля-идентификатора в value-структурах НЕТ.
- `Release`: `Labels map[string]string`, `Needs Needs`, `Chart{Name, Version}` (обязателен),
  `Values []FileRef`, `Store []FileRef`, `Options ReleaseOptions`
  (проброс nelm-опций: timeout, autoRollback≈atomic, createNamespace, …).
  *(блок `universal:`/`UniversalValues` отложен — §12.)*
- **`FileRef`** (единый для values и store): `Src`, `Dst` (только store), `Optional`, `Strict`.
  Принимает 4 формы (строка/мапа × со схемой/без).
- **`Sets []string`** — inline-оверрайды `key=value` (стиль helm `--set`), поверх values
  (высший приоритет); отдаются в nelm `ValuesSet`.
- **`Needs`**: `Releases map[string]NeedRelease` (`{Strict, …}`) + инлайн селектор
  `MatchLabels map[string]string` + `MatchLabelsExpressions []LabelSelectorRequirement`
  (семантика k8s; пустой = ничего).
- **Теги — двойные `json:"..." yaml:"..."`**: confijer биндит по `json`, planfile пишется по `yaml`.
  Теги `default:"..."` — там, где нужен дефолт (напр. `createNamespace:"true"`, `replicas:"1"`).

### 5.3 Контекст gomplate для `nelmwave.yml.tpl`
Согласуй минимальный набор: `.Env` (окружение), `.Project`, служебные функции gomplate
(`datasource`, `file`, `env`, `strings`, …). Реши, нужен ли доступ к per-release данным на этапе
рендера корневого файла (обычно нет — релизы разворачиваются уже из структуры).

---

## 6. Пайплайн `build` → `.nelmwave/`

`nelmwave build`:
1. Найти `nelmwave.yml.tpl` (или `nelmwave.yml`, если без шаблона). Рендер через gomplate (`[[ ]]`).
2. Загрузить результат в `config.Config` через confijer.
3. Валидация: уникальность uniqname (гарантируется ключами мапы), существование целей `needs`,
   отсутствие циклов в DAG (вкл. label-рёбра), корректность label-ключей/значений,
   обязательный `chart.name`.
4. Для каждого релиза: резолв **values** и **store files** через `internal/datasource`
   (render/copy; sops отложен). Values НЕ мёржатся — каждый источник → отдельный файл,
   список отдаётся в nelm (§9).
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

## 9. Values (собственный datasource-резолвер поверх gomplate v5)

- Каждый `FileRef.Src` — путь или URL, резолвится через `internal/datasource` (все схемы gomplate v5).
- По расширению источника: `.yml`→copy, `.yml.tpl`→gomplate-render, `.yml.sops`→**отложено** (§20 Q5).
- **4 эквивалентные формы записи** (нормализуются при parse): `{src: url}`, голая строка `url`,
  со схемой или без. Нет схемы или `file://` → голый локальный путь; прочие схемы (`env:`,
  `vault://`, `s3://`, `http(s)://`, `git://`, …) — verbatim.
- **Порядок и мёрдж** [обновлено]: nelmwave НЕ мёржит values сам — каждый источник резолвится в
  отдельный файл, и весь упорядоченный список отдаётся в nelm (`ValuesFiles`), который делает
  helm-native ordered deep-merge (мапы merge, скаляры/массивы replace, последний побеждает).
  Порядок: глобальные `Values` (корень config) → per-release `Values`.
- Артефакты — `.nelmwave/values/<uniqname>/<NN>-<label>.yml` (NN — индекс порядка), список путей
  пишется в planfile (`valuesFiles`).
- Флаги источника: `optional` (нет файла → пропустить, не падать), `strict`.

---

## 10. StoreFiles (тот же резолвер)

- `Release.Store []FileRef{ Src, Dst, ... }` — произвольные файлы, резолвятся тем же резолвером
  (правила copy/render; sops отложен) и складываются в `.nelmwave/store/<uniqname>/<Dst>`.
- Назначение: доп. манифесты и сопутствующие артефакты релиза (напр. NetworkPolicy, CRD, конфиги),
  которые нужно приложить/сохранить рядом с планом. Реши политику применения:
  - как дополнительные манифесты, подаваемые nelm вместе с релизом, **или**
  - просто складируются как артефакты (для аудита/дальнейших шагов).
  Согласуй — по умолчанию: **сохранять как артефакты**, применение доп. манифестов — отдельная опция.

---

## 11. Needs (зависимости)

### 11.1 Между релизами → DAG [FIXED]
- `Release.Needs` — **структура** (не `[]string`):
  - `needs.releases map[uniqname]NeedRelease` — явные зависимости по uniqname; значение
    расширяемое (сейчас `strict`).
  - `needs.matchLabels` + `needs.matchLabelsExpressions` — инлайн k8s-селектор; зависимость =
    все релизы, попавшие под селектор. Пустой селектор = ничего (НЕ «все»).
  - Итог: релиз зависит от **объединения** `releases` и label-матчей. `Config.DirectNeeds()`
    резолвит конкретный набор ключей.
- Построй ориентированный граф, проверь ацикличность (иначе ошибка build; цикл учитывает и
  label-рёбра), топосорт.
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

## 12. Встроенный универсальный chart [ОТЛОЖЕНО — пост-MVP]

> **Отложено** (решение владельца, 2026-08-04). В текущей схеме блока `universal:` НЕТ, `chart.name`
> обязателен. Раздел ниже — целевой дизайн на будущее (milestone после MVP).

- Обычный Helm-chart, **вшитый в бинарь** через `go:embed` (каталог `internal/chart/universal/`).
- Активируется, когда у релиза **не задан** `chart.name`, но задан блок `universal:`.
- Values универсального chart'а описываются **confijer-схемой**: top-level тип задаёт дефолты для
  всех релизов, каждый релиз переопределяет точечно (используй модель приоритетов confijer).
- **Охват ресурсов на старте** (реализуй именно этот набор, расширяемо):
  `Deployment`, `Service`, `Ingress`, `ConfigMap`, `Secret`, `HPA`. Заложи расширение
  (`ServiceAccount`, `PVC`, `CronJob`, `NetworkPolicy` — потом).
- Chart подаётся в `action.ReleaseInstall` как локальный путь (распакованный embed во временный
  каталог или `.nelmwave/charts/<release>/`).
- Поддержи аннотации needs-между-ресурсами (§11.2) прямо в шаблонах chart'а.

---

## 13. Repositories (helm-repo + OCI, объединены) [FIXED]

> **Изменено:** `registries` и `repositories` **слиты в одну мапу `repositories`** (ключ =
> alias/host). helm-repo (`https://`) и OCI (`oci://`) различаются по схеме URL (`Repository.IsOCI()`).
> Значение — голая строка-URL (короткая форма) или объект `{url, username, password, force_update,
> insecure_skip_tls_verify, pass_credentials, ca_file}`.

- **OCI registry** (`url: oci://host`): логин/креды прокинуть в nelm (registry-client опции);
  поддержи анонимный доступ.
- **Helm repository** (`url: https://…`): добавление/обновление repo, резолв `alias/chart`
  (chart.name = `<repo-alias>/<chart>`).
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

1. **M1 — Скелет + конфиг. ✅ Готово.** `go.mod` (Go 1.26), cobra-скелет, zap, `internal/config`
   со схемой (мапы/uniqname/FileRef/Needs), confijer-загрузка, gomplate-рендер `nelmwave.yml.tpl`
   (`[[ ]]`). Команда `build` рендерит, валидирует (обязательный chart.name, needs/циклы, labels),
   пишет `planfile.yml` в `.nelmwave/`. Тесты на парсинг/валидацию/рендер/канонизацию.
2. **M2 — Datasources. ✅ Готово.** Собственный `internal/datasource` поверх gomplate v5 (НЕ
   fileref): fetch всё через gomplate `include` (локальные пути → абсолютный `file://`, схемы как
   есть) + классификация по расширению (copy/`.tpl`-render/`.sops`-defer). Мёрдж values НЕ делаем —
   отдаём nelm список файлов. Оркестрация в `internal/build`: запись
   `.nelmwave/values/<uniqname>/<NN>-<label>.yml` и `store/<uniqname>/<dst>`, список `valuesFiles`
   в planfile. Тесты: `env:`, `.tpl`-рендер, sops-defer, missing→fs.ErrNotExist, optional-skip,
   порядок values.
3. **M3 — DAG + nelm up/down. ✅ Готово.** `internal/graph` (параллельный DAG-исполнитель:
   независимые ветки параллельно, fail-fast по ветке, `Reverse` для down), адаптер
   `internal/release` (`Applier` + `NelmApplier` поверх `action.ReleaseInstall/Uninstall`),
   `up`/`down` с селекцией `-l`, `--concurrency`, `--include-needs`, `--build`. Политика needs:
   строгий need вне выборки → ошибка, нестрогий → warn+drop, `--include-needs` дотягивает.
   k8s.io/* запинены на 0.29.x (nelm). Юнит-тесты на graph и deploy (fake Applier); реальный
   деплой — на kind (вне автотестов без кластера).
4. **M4 — diff/plan. ✅ Готово.** `action.ReleasePlanInstall` через `Applier.Plan`;
   `--detailed-exitcode` → exit 2 при изменениях (сентинелы nelm `ErrChangesPlanned` и др.;
   `cli.exitError{code}` в Execute). `up --dry-run` делегирует в diff. graph.Run ловит паники.
   ВАЖНО: перед `action.*` привязать логгер к ctx (`nelmlog.SetupLogging`), иначе паника logboek.
5. **M5 — Универсальный chart. [ОТЛОЖЕНО — пост-MVP]** `go:embed` chart, confijer-values, набор
   ресурсов из §12, активация через `universal:`. В MVP не входит; `chart.name` обязателен.
6. **M6 — Registries/repos. ✅ Готово.** `internal/repo`: `Resolve` мапит chart.name на nelm —
   `oci://`→passthrough, `alias/chart`→ChartRepoURL+basic-auth (helm `--repo`, без repositories.yaml),
   иначе локально; `DockerConfig` генерит временный docker config.json для OCI-кредов
   (`RegistryCredentialsPath`). ВАЖНО: удалённые чарты за feature gate — включаем
   `featgate.FeatGateRemoteCharts.Enable()` в init пакета release.
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
- ~~Универсальный chart разворачивает Deployment+Service+Ingress+ConfigMap+Secret+HPA из `universal:`.~~
  *(отложено — пост-MVP, §12.)*
- Values и store-files тянутся из ≥2 разных datasource-схем.
- README с быстрым стартом и описанием схемы `nelmwave.yml`.

---

## 20. Открытые вопросы для уточнения у владельца

Отметь и задай, если всплывёт по ходу (не блокируйся — предложи дефолт и продолжай):
1. Политика при `needs` в отфильтрованный релиз: ошибка vs авто-подтягивание (`--include-needs`)?
   *(дефолт: ошибка. Уточняется: `needs.releases.<uniqname>.strict` даёт per-need управление.)*
2. StoreFiles: только артефакты vs авто-применение доп. манифестов? *(дефолт: артефакты)*
3. Множественные `-l`: AND vs отдельные группы?
4. Точные nelm-опции для repo/registry override и аннотаций ресурсных зависимостей — что реально
   принимает API текущей версии nelm.
5. SOPS: **решено — отложить.** В MVP только `.yml`/`.yml.tpl`; `.yml.sops` → «not supported yet».
   Отдельных encrypt/decrypt-команд нет. (Пассивная расшифровка вернётся позже, без fileref.)

---

## 21. Жёсткие правила

- **Не** делай drop-in совместимость с `helmwave.yml` — схема новая.
- **Не** shell-out'и в бинарь nelm/gomplate — только Go-библиотеки.
- Делимитеры шаблонов — **`[[ ]]`** по умолчанию.
- Селекторы — **k8s-style**, парсинг через `apimachinery/labels`.
- Артефакты сборки — только в **`.nelmwave/`**; runtime-команды не перерендеривают шаблоны.
- Всё логируй через **zap**; весь I/O — через **context**.
- Внешние API (nelm/gomplate/confijer) — **сверяй с исходниками**, версии фиксируй в `go.mod`.
- **fileref НЕ используется** (v0.2.0 непригоден как библиотека) — datasources через свой резолвер
  на gomplate v5 (§3.4). Не тащить `metav1` ради мелочей — предпочитать лёгкие пакеты apimachinery
  (`pkg/labels`, `pkg/selection`).
- config-структуры — **двойные теги `json`+`yaml`** (confijer биндит по json; см. §5.2).
