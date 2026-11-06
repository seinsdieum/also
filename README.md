# also

**Один CLI для версионирования AI-инфраструктуры и переноса её между harness.**

`also` хранит ваши инструкции, скиллы, команды, агентов, хуки и MCP-серверы в git-репозитории в
универсальном виде, а при установке материализует их в нативный формат нужного harness:
Claude Code, OpenCode, Codex CLI или Qwen Code.

Одна и та же инфраструктура ставится в любой harness и остаётся управляемой: версии
иммутабельны, обновления проверяемы, конфиги не перезаписываются вслепую.

```
Claude bundle ──push──> also package (v=content hash) ──install -h opencode──> .opencode/
                                                     └──install -h codex─────> .agents/skills + .codex/
```

---

## Зачем

- Не нужно заводить отдельный git-репозиторий на каждую папку с инфраструктурой.
- Нет копирования «руками» и потери source of truth: пакет неизменяем и проверяется по хешу.
- Скиллы, написанные для Claude, ставятся в OpenCode/Codex/Qwen без ручного переписывания.
- Установка обратима: `also` помнит каждый созданный файл, блок инструкций и правку конфига.

## Архитектура: harness как адаптер

Перевод не размазан по установщику: каждый harness — отдельный адаптер
(`internal/also/adapters_<id>.go`), который описывает нативные пути,
возможности, форматы MCP/hooks/settings и диалекты команд/агентов. Система
отвечает за пакеты, digest, установку, обратимые правки и безопасность границ;
адаптер — за то, как именно harness представляет инфраструктуру.

Новый harness = новый файл-адаптер + `init()`-регистрация, без правок
`install`/`render`/`mcp`/`hooks`. Адаптеры проходят валидацию при регистрации:
уникальные имена, безопасные пути, известные форматы, обязательные
конфиги/события. Пути, выбранные адаптером, дополнительно ограничиваются
проектом (или `$HOME` для global) во время установки.

Подробности и шаблон: [`docs/ADAPTERS.md`](docs/ADAPTERS.md).

## Версии и чистота репозитория

Версия пакета — content hash, но она **не создаёт папку на каждую версию**.
В set-репозитории живёт ровно одно актуальное дерево пакета:

```
packages/<name>/
├── also-pkg.json      # метаданные, label, digest, created_at
└── origin/...         # файлы инфраструктуры
```

Каждая версия закреплена неизменяемым git-тегом
`refs/tags/also/<name>/<version>`. Старые версии достаются из истории git
(`git archive`), а не из копий каталогов. Поэтому:

- новая версия переписывает только одно дерево пакета;
- `install pkg@version` и `install pkg@label` работают через теги;
- `list --versions` видит всю историю, включая миграцию старого layout;
- репозиторий остаётся чистым: `packages/<name>/{also-pkg.json,origin}`.

Старый layout с `packages/<name>/<version>/` читается автоматически и
конвертируется командой `also set <name> --migrate`; миграция сохраняет
оригинальные timestamps версий в аннотированных тегах.

## Сборка

```sh
go build -o bin/also .
# или
make build && make install PREFIX=$HOME/.local
```

Требуется системный `git` (используются ваши SSH-ключи и credential helper) и Go 1.23+ для сборки.

## Быстрый старт

```sh
# 1. Подключить существующий пустой/непустой git-репозиторий как набор
also set personal --remote git@github.com:you/ai-infra.git --source ~/Development/ai-infra --default

# 2. Закодировать и запушить одну из ваших папок-бандлов
also push ~/Development/ai-infra/code-review

# 3. Выбрать harness по умолчанию (иначе указывайте -h при каждой установке)
also config set default_harness opencode

# 4. Установить в текущий проект
also install code-review

# 5. Посмотреть, что реально установлено
also tree
```

Пример `~/Development/ai-infra/code-review` для Claude:

```
code-review/
├── CLAUDE.md
├── skills/reviewer/SKILL.md
├── commands/review.md
├── agents/critic.md
├── hooks/guard.sh
├── settings.json          # hooks (и прочие настройки — сохраняются, но не все ставятся)
└── .mcp.json
```

## CLI как язык

`also` говорит на коротком английском: сначала глагол, потом объект, условия —
обычными словами. Флаги остались для скриптов и сложных случаев, но не нужны
для повседневной работы.

```sh
also use personal                                  # активный namespace
also install awesome-design for opencode           # пакет в конкретный harness
also install reviewer skill from personal@code-review for opencode
also install guard hook from code-review           # только один hook
also install github mcp from personal@tools        # только один MCP-сервер
also exclude infra                                 # harness-файлы в .gitignore
also include you                                   # убрать метаданные also из .gitignore
also add harness ./adapters/pi.json                # community-harness
```

Если harness не выбран и значения по умолчанию нет, `also` не угадывает:

```
$ also install awesome-design for
Which harness? Please clarify!
You've got: claude, codex, opencode, qwen
Try: also install <package> for <harness>
```

`namespace` — это настроенный набор (`set`). Активный namespace определяет,
куда уйдут `push`/`adopt` и откуда по умолчанию читаются пакеты. Точечно
namespace указывается как `personal@package` или словами `from personal`.

Выборочная установка записывается в состояние (`parts`): `also upgrade`
обновляет только выбранные части и не добавляет компоненты, которые вы не
просили. `adopt` при этом забирает всё, что реально лежит в проекте —
установленные части и новые файлы, созданные вами.

Один и тот же пакет можно держать в нескольких harness одного проекта: у
каждой пары пакет/harness свой слот состояния, поэтому установка в OpenCode
не удаляет установку в Claude.

## Команды

| Команда | Что делает |
|---|---|
| `also use [namespace]` | Показать namespace или сделать его активным. |
| `also set <name> --remote <url> [--source <dir>] [--default]` | Подключить набор. Локальный клон: `~/.also/sets/<name>`. |
| `also set <name> --migrate` | Перевести старый layout `packages/<pkg>/<version>/` на одно дерево + git-теги. |
| `also set` | Показать настроенные наборы. |
| `also config` / `also config set <key> <value>` | `default_set`, `default_harness`, `default_scope`. |
| `also list [--refresh] [--versions] [--from <url>]` | Пакеты и версии в наборе. |
| `also show <pkg>[@version]` | Манифест, компоненты, MCP/hooks, версии. |
| `also push <bundle-dir> [for <harness>] [--name N] [--label v1.2]` | Закодировать бандл, закоммитить и запушить новую версию. |
| `also install <pkg>[@version] for <harness>` | Развернуть пакет в нативный формат. |
| `also install <part> <kind> from [namespace@]<pkg> [for <harness>]` | Установить одну часть: `skill`, `command`, `agent`, `plugin`, `hook`, `mcp`, `settings`, `instructions` или custom-kind. |
| `also exclude\|include infra [for <harness>] [in <dir>]` | Управлять `.gitignore` для harness-инфраструктуры проекта. |
| `also exclude\|include you` | Управлять `.gitignore` для `.also-layers/` и `.also/`. |
| `also add harness <file\|url>` / `also remove harness <id>` | Подключить или убрать community-адаптер. |
| `also list harnesses` | Показать built-in и community harness. |
| `also upgrade\|update [pkg...] [--check] [--force]` | Обновить установленное (для того harness, куда ставили). |
| `also reshape <harness> [pkg...] [--force]` | Пересобрать установленное в другой harness, сохранив версию. |
| `also tree [for <harness>] [--json]` | Дерево **только установленной** инфраструктуры. |
| `also adopt <project-dir> [for <harness>] [--name N] [--into <dir>]` | Забрать инфраструктуру живого проекта в локальный бандл и запушить. |
| `also uninstall <pkg> [for <harness>] [--scope] [--project] [--all] [--force]` | Удалить установленное и откатить правки конфигов. |

Полезные флаги:

- `for <harness>` — основной способ выбрать harness; `-h/--harness` оставлен для скриптов. **`-h` — это harness, а не help**; справка — `--help`.
- `--scope project` — по умолчанию (см. `default_scope`); `--scope global` — конфиг пользователя.
- `--force` — перезаписать изменённые вручную файлы/конфликты. Перед перезаписью делается бэкап.
- `--no-config` — ставить только портируемые компоненты, не трогать MCP/hooks.
- `--offline` — не ходить в сеть; если локального клона нет, команда завершится ошибкой.
- `--from <url>` — установить/посмотреть без настройки набора (нужен только read-доступ).
- `--allow-secrets`, `--allow-large` — явно разрешить то, что по умолчанию отклоняется.

## Что такое версия

Версия — не ручной semver, а **content hash**:

```
version = sha256(schema, name, description, source_harness, label, все файлы origin)[:16]
```

Свойства:

- Одинаковое содержимое → одна и та же версия, повторный `push` — no-op.
- Изменение любого файла, описания или label → новая версия.
- Пакет проверяется при установке: `also-pkg.json.digest` должен совпасть с содержимым.
- `@latest` — версия с самым новым git-коммитом, `@v1.2` — человеческий label, `@a1b2c3` — префикс хеша.

`--label` — только удобный алиас. Он не влияет на порядок: «новее» = позже закоммичено.
Это осознанный компромисс: content-addressed версии не требуют ручного бампа и не дают
«забыл поднять версию».

## Layout удалённого репозитория

```
ai-infra.git/
├── also-set.json
├── packages/
│   └── code-review/
│       ├── also-pkg.json          # метаданные последней версии, digest, created_at
│       └── origin/                # байт-в-байт то, что вы запушили
│           ├── CLAUDE.md
│           ├── skills/...
│           ├── commands/...
│           ├── agents/...
│           ├── hooks/...
│           ├── settings.json
│           └── .mcp.json
└── refs/tags/also/code-review/<version>   # неизменяемые снимки всех версий
```

- Один репозиторий = один набор (`set`/namespace) = много пакетов.
- В рабочем дереве живёт только последняя версия каждого пакета; старые версии
  достаются из git-тегов, а не из копий каталогов.
- Версии только добавляются; `also` никогда не перезаписывает существующую версию.
- Старый layout `packages/<pkg>/<version>/` читается автоматически и переводится
  командой `also set <name> --migrate`.
- `also-set.json` и `packages/**` — единственные управляемые пути. Ваши локальные бандлы
  остаются там, где вы их держите; `--source` — только ссылка на них.

Полная спецификация: [docs/PACKAGE_FORMAT.md](docs/PACKAGE_FORMAT.md).

## Поддерживаемые harness

| Harness | Instructions | Skills | Commands | Agents | Plugins | Hooks | MCP |
|---|---|---|---|---|---|---|---|
| Claude Code | `CLAUDE.md` | `skills/` | `commands/` | `agents/` | — | `settings.json` | `~/.claude.json`, `.mcp.json` |
| OpenCode | `AGENTS.md` | `skills/` | `commands/` | `agents/` | `plugins/` | plugins (код) | `opencode.json(c)` → `mcp.servers` (или legacy `mcp.<name>`) |
| Codex CLI | `AGENTS.md` | `.agents/skills/` | — | — | — | `hooks.json` | `config.toml` → `[mcp_servers.*]` |
| Qwen Code | `QWEN.md` | `skills/` | `commands/` | `agents/` | — | `settings.json` | `settings.json` → `mcpServers` |

Пути указаны относительно корня конфигурации harness; для project scope — относительно проекта.
Точные пути — в `internal/also/harness.go`. Community-адаптеры добавляют свои harness
без пересборки: `also add harness <file|url>`

## MCP и hooks: что реально транслируется

**MCP** нормализуется в общую модель (`stdio` / `http`, command, env, url, headers) и
записывается в нативный формат цели. Поддерживаются все четыре harness.

**Hooks** транслируются между декларативными форматами Claude / Codex / Qwen
(общие события вроде `PreToolUse`, `PostToolUse`, `Stop`, `SessionStart`). События, которых
нет у цели, пропускаются с предупреждением.

Ограничения, о которых нужно знать:

- **OpenCode не имеет декларативных hooks** — это JS/TS-плагины. При установке в OpenCode
  hooks пропускаются с предупреждением. Плагины OpenCode копируются как файлы только
  opencode → opencode.
- Тела hook-команд копируются **как есть**. Переменные вида `$CLAUDE_PROJECT_DIR` и пути
  к скриптам не переписываются; установщик предупреждает об этом.
- Namespace в Qwen: `$ARGUMENTS` ↔ `{{args}}`, `` !`cmd` `` ↔ `!{cmd}` переводятся;
  позиционные `$1..$9` копируются с предупреждением.
- Claude/Qwen требуют подтверждения project MCP-серверов; Codex загружает project
  `.codex` только в trusted-проекте; OpenCode при долго работающем service может требовать
  `opencode reload`. Эти предупреждения выводятся при установке.
- Вложенные `AGENTS.md` (по директориям) в v1 не устанавливаются: только корневой.
- Inline-определения агентов/команд внутри `opencode.json` не извлекаются в пакет.
  Используйте файловые `agents/*.md` и `commands/*.md`, если нужна переносимость.
- Permissions, модели по умолчанию, темы, провайдеры и прочие настройки сохраняются в
  пакете, но не устанавливаются: `also` не переписывает чужие policy-настройки.

## Надёжность и безопасность

- **Пакет не может писать вне целевой директории harness**: пути валидируются, `..` и
  абсолютные пути отклоняются, symlink'и в пакете запрещены.
- **Секреты по умолчанию не уезжают**: `.env*`, `*.pem`, `*.key`, `auth.json`, БД и т.п.
  блокируются. Литеральные значения MCP `env`/`headers` с ключами вида `API_KEY`,
  `TOKEN`, `Authorization` блокируются отдельно. Обойти — только явным `--allow-secrets`.
- **Managed blocks**: инструкции пакета живут в файле между маркерами
  `<!-- also:begin set/pkg -->` / `<!-- also:end set/pkg -->`. Ваш текст вокруг не трогается.
- **Reversible config edits**: каждая правка MCP/hooks/settings записывается в состояние. При
  `upgrade` старая правка снимается, при `uninstall` — восстанавливается предыдущее значение.
  Если значение изменили вы — без `--force` она не будет тронута. Файл конфигурации,
  созданный `also` и ставший пустым после удаления, удаляется.
- **Native layers для round-trip**: `adopt` кладёт неподдерживаемые промежуточным harness
  части в `.also-layers/<harness>/` вместе с манифестом baseline-хешей. При возврате в
  исходный harness native-файл побеждает, **только если** переведённая копия не менялась;
  локальные правки в промежуточном harness сохраняются.
- **Конфликты**: если файл уже существует и не принадлежит `also`, установка по умолчанию
  падает. `--force` перезаписывает, сохраняя копию в `~/.also/backups/<timestamp>/`.
- **Транзакционность**: все файлы и конфиги меняются через журнал; при ошибке на любом шаге
  выполняется rollback. Состояние пишется атомарно (temp + rename).
- **Блокировка**: изменяющие команды сериализуются через `~/.also/lock`.
- **Git-семантика**: версия становится доступной после коммита и push. Если push не удался,
  коммит остаётся локально; повторный `also push` дошлёт его.
- **Никакой магии с remote**: `also` пишет только в remote, настроенный для набора, и
  проверяет, что локальный клон смотрит именно туда.

## Конфигурация и состояние

```
~/.also/
├── config.json        # наборы, default_harness, default_scope
├── state.json         # установки, файлы, блоки, правки конфигов
├── sets/<name>/       # git-клоны наборов
├── cache/<hash>/      # клоны для --from <url>
├── backups/<ts>/      # копии перезаписанных файлов
└── lock
```

`ALSO_HOME` переопределяет корень (удобно для тестов и изоляции).

Пример конфига:

```json
{
  "schema": "also.config/v1",
  "default_set": "personal",
  "default_harness": "opencode",
  "default_scope": "project",
  "sets": {
    "personal": {
      "remote": "git@github.com:you/ai-infra.git",
      "path": "/Users/you/.also/sets/personal",
      "source": "/Users/you/Development/ai-infra"
    }
  }
}
```

## Ограничения v1

- Только один remote на набор; remote-репозиторий должен уже существовать.
- Нет автоматического `gh repo create`.
- `adopt` работает с project-scope раскладкой harness; глобальные конфиги не забирает.
- Symlink'и в бандлах не поддерживаются (явная ошибка вместо тихого копирования).
- Windows проверен на Windows 11 ARM64 (portable MinGit): clone set, install/uninstall,
  cross-harness и round-trip пакеты. Нужен установленный `git`; harness CLI на Windows
  не проверялись (их проверка выполнена на macOS).
- Set-клон принудительно использует LF (`core.autocrlf=false`), иначе на Windows
  ломается content digest.
- Content hash ≠ semver: порядок версий определяется git-историей, а не номером.
- Два пакета не могут владеть одним и тем же файлом скилла/команды/агента: второй
  `install` упадёт с конфликтом. Переименуйте компонент в одном из пакетов.
- Общие MCP-серверы и hook-группы: если значение уже настроено (вами или другим пакетом),
  `also` не создаёт дубликат и не претендует на владение. При удалении пакета, который
  создал значение, оно будет восстановлено/удалено по правилам его собственной записи.
- Cross-harness permissions **не транслируются автоматически** (риск выдать лишние права):
  они сохраняются в universal-пакете/native-слое и возвращаются при установке в исходный harness.
- Новый harness добавляется адаптером (`docs/ADAPTERS.md`). Built-in адаптеры
  компилируются в бинарник; community-адаптер можно подключить без сборки:
  `also add harness <file|url>`. Динамической загрузки стороннего кода нет:
  адаптер выбирает только зарегистрированные форматы и пути, а система
  проверяет их и ограничивает запись.
- Бандлы без однозначных маркеров (`CLAUDE.md`, `QWEN.md`, `opencode.json`, `.codex`/`config.toml`)
  требуют явного `for <harness>` или `-h/--harness`: например, одинокий `AGENTS.md` не говорит, Codex это или OpenCode.

## E2E-проверка

Реальный стресс-прогон на приватном git-репозитории (4 инфраструктуры × 4 harness,
16 обратных round-trip через `adopt`) и проверка установки самими CLI описаны в
[docs/E2E_REPORT.md](docs/E2E_REPORT.md). Итог: 16/16 round-trip совпали;
Claude/Codex/OpenCode/Qwen загружают инструкции, skills и hooks из установленных файлов.

## Разработка

```sh
make test      # go test ./...
make vet
make build
```

Тесты поднимают временный bare-репозиторий и проверяют полный цикл:
push → install → upgrade → reshape → uninstall, managed blocks, MCP/hooks merge,
восстановление предыдущих значений и отказ при локальных изменениях.

## Структура

```
internal/also/       ядро: git, пакеты, harness-адаптеры, рендеринг, установка
internal/cli/        cobra-команды
main.go
docs/PACKAGE_FORMAT.md
```
