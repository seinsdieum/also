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

## Команды

| Команда | Что делает |
|---|---|
| `also set <name> --remote <url> [--source <dir>] [--default]` | Подключить набор. Локальный клон: `~/.also/sets/<name>`. |
| `also set` | Показать настроенные наборы. |
| `also config` / `also config set <key> <value>` | `default_set`, `default_harness`, `default_scope`. |
| `also list [--refresh] [--versions] [--from <url>]` | Пакеты и версии в наборе. |
| `also show <pkg>[@version]` | Манифест, компоненты, MCP/hooks, версии. |
| `also push <bundle-dir> [--name N] [--label v1.2] [--harness H]` | Закодировать бандл, закоммитить и запушить новую версию. |
| `also install <pkg>[@version] [-h harness] [--scope global\|project]` | Развернуть пакет в нативный формат. |
| `also upgrade [pkg...] [--check] [--force]` | Обновить установленное (для того harness, куда ставили). |
| `also reshape <harness> [pkg...] [--force]` | Пересобрать установленное в другой harness, сохранив версию. |
| `also tree [--json]` | Дерево **только установленной** инфраструктуры. |
| `also adopt <project-dir> [--name N] [-h harness] [--into <dir>]` | Забрать инфраструктуру живого проекта в локальный бандл и запушить. |
| `also uninstall <pkg> [--scope] [-h] [--project] [--all] [--force]` | Удалить установленное и откатить правки конфигов. |

Полезные флаги:

- `-h, --harness` — целевой harness (`claude`, `opencode`, `codex`, `qwen`). **`-h` — это harness, а не help**; справка — `--help`.
- `--scope project` — по умолчанию (см. `default_scope`); `--scope global` — конфиг пользователя.
- `--force` — перезаписать изменённые вручную файлы/конфликты. Перед перезаписью делается бэкап.
- `--no-config` — ставить только портируемые компоненты, не трогать MCP/hooks.
- `--offline` — не ходить в сеть, использовать локальный клон.
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
└── packages/
    └── code-review/
        ├── 54883c0efc5b4330/
        │   ├── also-pkg.json
        │   └── origin/            # байт-в-байт то, что вы запушили
        │       ├── CLAUDE.md
        │       ├── skills/...
        │       ├── commands/...
        │       ├── agents/...
        │       ├── hooks/...
        │       ├── settings.json
        │       └── .mcp.json
        └── a8f2dda2a8a51341/
            └── ...
```

- Один репозиторий = один набор (`set`) = много пакетов.
- Версии только добавляются; `also` никогда не перезаписывает существующую версию.
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
Точные пути — в `internal/also/harness.go`.

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
- **Reversible config edits**: каждая правка MCP/hooks записывается в состояние. При
  `upgrade` старая правка снимается, при `uninstall` — восстанавливается предыдущее значение.
  Если значение изменили вы — без `--force` она не будет тронута. Файл конфигурации,
  созданный `also` и ставший пустым после удаления, удаляется.
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
- Windows не тестировался (пути harness рассчитаны на macOS/Linux).
- Content hash ≠ semver: порядок версий определяется git-историей, а не номером.
- Два пакета не могут владеть одним и тем же файлом скилла/команды/агента: второй
  `install` упадёт с конфликтом. Переименуйте компонент в одном из пакетов.
- Общие MCP-серверы и hook-группы: если значение уже настроено (вами или другим пакетом),
  `also` не создаёт дубликат и не претендует на владение. При удалении пакета, который
  создал значение, оно будет восстановлено/удалено по правилам его собственной записи.
- Бандлы без однозначных маркеров (`CLAUDE.md`, `QWEN.md`, `opencode.json`, `.codex`/`config.toml`)
  требуют явного `-h/--harness`: например, одинокий `AGENTS.md` не говорит, Codex это или OpenCode.

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
