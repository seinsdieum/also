# also — e2e stress report

Дата: 2026-11-06. Версия `also`: 0.1.0 + исправления из этого прогона.

## Полигон

- Приватный git-репозиторий: `https://github.com/seinsdieum/also-e2e-20261106` (private).
  В нём лежат пакеты `claude-real`, `codex-real`, `opencode-real`, `qwen-real`
  и 16 round-trip пакетов `rt*`. Удалить: `gh repo delete seinsdieum/also-e2e-20261106`.
- Инфраструктура взята AS IS из публичных источников, без ручного переписывания:
  - `anthropics/skills` — skills (`skill-creator`, `theme-factory`, `webapp-testing`, ...);
  - `obra/superpowers` — skills, реальный OpenCode plugin (`plugins/superpowers.js`),
    реальный `hooks/hooks.json` для Codex;
  - `wshobson/agents` — agents, commands, skills, `AGENTS.md`/`CLAUDE.md`;
  - `openai/skills` — Codex skills;
  - `QwenLM/qwen-code` — реальный `AGENTS.md` (использован как `QWEN.md`);
  - `disler/claude-code-hooks-mastery` — реальные Claude hooks, `settings.json`
    с permissions/statusLine/hooks, `status_lines`, вложенные agents.
- Установлены/использованы CLI: Claude Code 2.1.318, Codex CLI 0.163.0,
  OpenCode 2.2.5, Qwen Code 0.25.2. Модельные запросы шли в локальный stub
  (`/v1/messages`, `/v1/responses`, `/v1/chat/completions`), без реальной аутентификации.

## Что прогонялось

1. Каждая из 4 инфраструктур закодирована и запушена в приватный set.
2. Каждая инфраструктура установлена в каждый из 4 harness в **project scope**
   (16 установок).
3. Для каждой пары X→Y: проект `Y` забирался через `also adopt` в новый bundle
   (с native-слоем и манифестом), пушился как `rt-*`, затем ставился обратно в `X`.
4. Сравнивались reference (X→X) и reverse (X→Y→X) проекты:
   instructions, skills, commands, agents, plugins, extras, hooks,
   MCP-серверы и permissions/statusLine.

Итог: **16/16 round-trip полностью совпали** по перечисленным артефактам
(см. `compare5.out` в e2e-каталоге; каждая строка `RESULT OK`).

## Найденные и исправленные дефекты

| # | Дефект | Исправление |
|---|---|---|
| 1 | При установке в тот же harness терялись «неизвестные» hook-события (`PermissionRequest`, `PostToolUseFailure`, `Setup`, `SubagentStart`) | same-harness hooks теперь identity; списки событий расширены |
| 2 | `permissions` и `statusLine` сохранялись в пакете, но не устанавливались даже в тот же harness | same-harness merge с обратимым state (`json-append`/`json-set`); cross-harness — warning + сохранение |
| 3 | `status_lines`/`output-styles`/`themes`/`extensions` не попадали в пакет и не ставились | opaque `ExtraDirs`: capture в adopt, установка same-harness, сохранение в universal |
| 4 | `adopt` удалял managed-блок целиком; проект, состоящий только из установленного блока, терял instructions | блоки разворачиваются (маркеры удаляются, тело сохраняется) |
| 5 | Round-trip X→Y→X терял permissions/hooks/agents/commands, которые Y не умеет или переводит иначе | `.also-layers/<harness>/` + `.also-layers/manifest.json`: native-слой восстанавливает исходник, если переведённый файл не менялся; изменённый в Y файл имеет приоритет |
| 6 | MCP при same-harness терял незнакомые поля (`disabled`, `timeout`, ...) | `MCPServer.Raw`: same-harness пишет native-объект; Codex — пере-кодирует TOML |
| 7 | Несколько Codex MCP-серверов давали повторный `[mcp_servers]` и невалидный TOML | parent-заголовок удаляется из каждого блока |
| 8 | Qwen command placeholders не переводились (`$ARGUMENTS`, `` !`cmd` ``) | `$ARGUMENTS` ↔ `{{args}}`, `` !`cmd` `` ↔ `!{cmd}`; warning для `$1..$9` |
| 9 | Два push в одну секунду давали неверный `latest` (git timestamp 1s + лексикографический tie-break) | tie-break по порядку в `git log` |
| 10 | OpenCode legacy `mcp.<name>` не поддерживался | запись в ту же форму, что уже использует конфиг |
| 11 | `adopt` не получал доступ к оригинальному пакету для сохранения неподдерживаемых частей | state install → set clone → origin → native layer |
| 12 | Windows-клон set с `core.autocrlf=true` получал CRLF, из-за чего не сходился content digest | clone с `core.autocrlf=false`/`core.eol=lf`; существующий чистый клон автоматически переписывается в LF |
| 13 | Digest учитывал exec-bit рабочей копии; на Windows его нет, поэтому Unix-пакеты не проходили проверку | режимы файлов берутся из git index (100644/100755), а не из worktree |
| 14 | На Windows `filepath.Dir` давал `skills\x` вместо `skills/x`; skills попадали в «not a portable component» и не ставились | для путей пакета используется slash-aware `path.Dir` |

## Адаптерная архитектура и повторный E2E

Harness-специфичная логика вынесена из `install/render/mcp/hooks/settings` в
адаптеры: `internal/also/adapter.go` + `adapters_<id>.go`. Система владеет
пакетами, digest, установкой, обратимыми правками и границами путей; адаптер
владеет нативными путями, возможностями, форматами MCP/hooks/settings,
диалектами команд/агентов, detection-правилами и install-hints.

- `registerAdapter` валидирует адаптер при старте: уникальные id/aliases,
  безопасные относительные пути, известные форматы, обязательные каталоги,
  события и target-функции, TOML-конфиги для `codex-toml`.
- `ValidateAdapters` проверяет весь реестр; `TestAdaptersAreValid`,
  `TestAdapterValidationRejectsUnsafeSpecs`, `TestAdapterReusesExistingFormats`
  и `TestConfigPathIsBounded` покрывают контракт.
- Во время установки система независимо проверяет, что адаптер не вывел файлы
  за пределы проекта (`fileAllowedRoot`) и конфиги — за пределы проекта/`$HOME`
  (`configPathAllowed`).
- Новый harness, совместимый с существующим экосистемным форматом, добавляется
  одним файлом-адаптером и `init()`; generic-код не меняется.

После рефакторинга полный E2E прогнан заново, включая не только установку, но
и `adopt` → `push` → обратную установку:

1. Для каждого X собран reference: `X-real` → harness X.
2. Для каждой пары X→Y: `X-real` → harness Y, затем `adopt` в пакет
   `rt6-<X>-real-to-<Y>` и `push` в приватный set.
3. `rt6-<X>-real-to-<Y>` установлен обратно в X (`back6-*`).
4. Comparator сравнил instructions, skills, commands, agents, plugins, extras,
   hooks, MCP и permissions/statusLine.

Итог: **16/16 `RESULT OK`** (`compare6.out`), полный цикл adopt/push не потерял
данные. Дополнительно `make check` (gofmt, `go vet`, `go test`) — чисто.

## Версионирование без папок на версию

Старый layout `packages/<pkg>/<version>/` заменён:

- `packages/<pkg>/{also-pkg.json,origin}` — только актуальное дерево;
- `refs/tags/also/<pkg>/<version>` — неизменяемый snapshot каждой версии;
- старые версии читаются из git через `git archive`;
- `created_at` в манифесте и аннотированные теги сохраняют порядок версий;
- `also set <name> --migrate` конвертирует старый layout, сохраняя даты.

Проверено на реальном set: миграция всех пакетов, **0** лишних version-каталогов,
72+ тегов, `install` старых и новых версий, свежий clone с GitHub.

После этого полный E2E прогнан ещё раз с пакетами `rt7-*`:
`install → adopt → push → reverse install` для всех 16 пар — **16/16 `RESULT OK`**
(`compare7.out`).

Дополнительно проверен внешний декларативный адаптер `acme` (JSON в
`ALSO_ADAPTERS`): `push --harness acme` → `install --harness acme` → `adopt`
вернул `AGENTS.md`, skills, commands, agents, settings, MCP и custom-компонент
`workflows/deploy.md`. Это подтверждает, что новый harness можно добавить без
перекомпиляции, если он переиспользует зарегистрированные форматы.

## Проверка «на уровне софта»

- **Claude Code**
  - `claude mcp list` в установленном проекте:
    `fetch: uvx mcp-server-fetch - ⏸ Pending approval`, `remote: ... - ⏸ Pending approval`.
  - `claude -p "Reply with OK"` со stub: `OK`; в теле запроса к модели присутствуют
    текст `CLAUDE.md`, имена skills, agent `backend-architect`, command
    `feature-development`.
  - SessionStart-hook из установленного `settings.json` создал marker-файл.
- **Codex CLI**
  - С trusted-проектом (`[projects."<path>"] trust_level = "trusted"`) и stub `CODEX_HOME`:
    `codex mcp list` показывает `fetch` из project `.codex/config.toml` (и global `ghidra`).
  - `codex exec` со stub: `OK`; в запросе есть `AGENTS.md` (`seven coding tools`),
    skills `migrate-to-codex`, `security-threat-model`, `skill-creator`.
  - SessionStart-hook создал marker-файл.
- **OpenCode 2.2.5**
  - `opencode debug agents` показывает установленного `backend-architect`.
  - `opencode debug config` показывает `mcp.servers.fetch/remote` и `permissions`
    из project `opencode.json`.
  - `opencode mcp list` после `opencode reload`: `fetch` connected; `remote` не резолвится
    (пример URL), `ghidra` — из глобального конфига пользователя.
  - `opencode --standalone run` со stub: `OK`; в запросе есть `AGENTS.md` и skills
    `writing-plans`, `receiving-code-review`.
- **Qwen Code 0.25.2**
  - `qwen mcp list`: `fetch` и `remote` — Pending approval.
  - `qwen -p` со stub: `OK`; в запросе есть `QWEN.md` (`Working Principles`,
    `Simplicity First`), skills, agent `backend-architect`, command `feature-development`.
  - SessionStart-hook создал marker-файл.

## Политика harness, которую нужно учитывать

- Claude Code и Qwen Code требуют подтверждения project MCP-серверов
  (`Pending approval`). Это их политика, не ошибка установки.
- Codex загружает project `.codex/config.toml` и `hooks.json` только в **trusted**
  проекте. `also install` теперь предупреждает об этом.
- OpenCode подхватывает project config, но долго работающий service может требовать
  `opencode reload`.
- Cross-harness permissions намеренно не транслируются автоматически (риск
  выдать лишние права). Они сохраняются в universal-пакете и восстанавливаются
  native-слоем при возврате в исходный harness.

## Parallels / Windows

VM: Parallels `Windows 11`, Windows 10.0.26300, ARM64. В VM не было git, поэтому
поставлен portable `MinGit 2.56.0.4 arm64` (без установщика) в `C:\git`;
`also` собран как `windows/arm64` и запускался из `C:\Windows\Temp\also.exe`.
Set-репозиторий подключался по shared folder (`\\Mac\Home\Downloads\also-e2e.git`),
т.е. проверялся настоящий `git clone`.

Проверено на Windows:

- `also version`, `--help`, `config get/set` (JSON пишется в `ALSO_HOME`), `tree`;
- `also set` → clone приватного набора;
- `also install claude-real --harness claude`: 85 файлов, instructions-блок,
  hooks/permissions/statusLine/MCP, 5 skills;
- `also install claude-real --harness codex`: `AGENTS.md`, TOML MCP, hooks, 5 skills;
- `also install rt2-codex-real-to-claude --harness claude`: 33 файла, 3 skills;
- `also uninstall claude-real`: установленные файлы и managed config откатились.

Именно этот прогон выявил дефекты 12–14 (CRLF, exec-bit digest, path separator);
после исправлений все перечисленные установки проходят.

После адаптерного рефакторинга Windows-проверка повторена: свежий клон set и
`install rt6-claude-real-to-codex --harness claude` дали 85 файлов, 5 skills,
3 agents и `.mcp.json`; `also version` подтвердил, что реестр адаптеров
валиден и на Windows.

Ограничение: в VM нет Claude/Codex/OpenCode/Qwen CLI, поэтому проверка
«на уровне софта» для них выполнена на macOS (раздел выше). Windows-прогон
проверяет сам `also` и его git/config/filesystem-операции.

## Повторный E2E после audit-фиксов

После исправлений (guard для пустого upgrade/reshape, проверка правок TOML,
публикация legacy-тегов, защита от malformed digest, OpenCode legacy MCP,
слоты по harness, offline, кэш версий) полный приватный прогон выполнен
заново с пакетами `rt8-*`:

1. Для каждого X собран `ref8-X-real-X`.
2. Для всех 16 пар X→Y: установка в Y, `adopt` → `rt8-X-real-to-Y` → `push`,
   затем установка обратно в X (`back8-X-real-from-Y`).
3. `compare8.py` сравнил instructions, skills, commands, agents, plugins,
   extras, hooks, MCP и permissions/statusLine.

Итог: **16/16 `RESULT OK`**, `0 DIFFER` (`compare8.out`).
`also list` на том же наборе ускорился с 7.59 s до 2.79 s.

## In-repo matrix

`test/e2e/matrix_test.go` запускает ту же идею 4×4 без приватного репозитория:
для каждого source harness собирается бандл (instructions + skill), затем для
всех 16 пар выполняется `push → install → adopt → push → install back`.
Проверяется, что instructions и тело skill вернулись в исходный harness.
Матрица запускается обычным `go test ./...` в CI на macOS/Linux/Windows.

## Remote-only E2E (compare9)

После перехода на remote-only namespaces матрица прогнана заново на `rt9-*`:
`adopt` вызывался без `--into` и публиковал временный бандл прямо в namespace;
старый `also.config/v1` из приватного стенда мигрировал в v2 автоматически.

Итог: **16/16 `RESULT OK`**, `0 DIFF` (`compare9.out`). Это подтверждает, что
удаление локального source-каталога не изменило round-trip.

## Real harness CLI smoke

`make smoke` (`test/e2e/harness-smoke.sh`) собирает бандл с инструкциями,
skill и MCP-сервером, ставит его во временные проекты и проверяет реальными
CLI без запуска моделей:

- Claude Code: `claude mcp list` видит `smoke`;
- OpenCode: `opencode debug config` (или JSON, если сервис занят) видит `smoke`;
- Codex: `codex mcp list` парсит сгенерированный TOML (через изолированный
  `CODEX_HOME`; project-layer Codex читает только в trusted-проектах);
- Qwen: `qwen mcp list` видит `smoke`.

Локальный прогон 2026-11-06: `OK claude`, `OK opencode`, `OK codex`, `OK qwen`.

## Interactive setup

`also setup` проверен на пустом `ALSO_HOME` через pty: harness, scope, имя и
URL namespace, отказ от второго namespace, default namespace и пустой starter
package. Команда завершилась с кодом 0 и записала `also.config/v2` с одним
namespace; `~/.also/sets` не создавался.
