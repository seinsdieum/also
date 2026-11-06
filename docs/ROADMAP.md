# also roadmap

Статус: 2026-11-06, версия `1.0.0` (release candidate в работе). Это не обещание
сроков, а порядок работ и критерии готовности.

## Текущее состояние

Уже работает:

- harness-адаптеры для Claude Code, OpenCode, Codex и Qwen;
- universal package format (`also.pkg/v1`), content-hash версии и разные
  представления: живёт одно дерево `packages/<pkg>/{also-pkg.json,origin}`,
  старые версии достаются из git-тегов `refs/tags/also/<pkg>/<version>`;
- install / upgrade / reshape / uninstall / adopt / push / tree / show / list;
- managed instruction blocks и обратимые правки MCP, hooks, settings;
- native layers для round-trip между harness;
- partial install (`also install review skill from namespace@pkg`);
- multi-harness install одного пакета в одном проекте;
- community-адаптеры через `also add harness <file|url>`;
- естественный CLI-язык (`also install pkg for opencode`, `also use ns`);
- 4×4 E2E: 16/16 round-trip, `compare8.out`;
- remote-only namespaces: `also connect <name> <git-url>`, единый внутренний кэш;
- `also setup` (интерактивный и `--yes`) и `install.sh`/`install.ps1`;
- `adopt` публикует прямо в namespace без локального source-каталога;
- importable module, MIT license, CI и release workflow;
- `go test -race`, `make check`, покрытие ядра ~74.5%.

Чего пока нет как продукта:

- подписанных бинарников и пакетных менеджеров;
- стабильных контрактов state/adapter и политики миграций;
- доверия к community-адаптерам (подписи/checksum/подтверждение);
- диагностики (`also doctor`/`verify`) и документированного recovery;
- JSON-вывода и зафиксированных exit codes у изменяющих команд.

## Решения для 1.0

### D1. Namespace — только удалённый git-репозиторий

Отказываемся от пользовательского локального set и `--source`. Namespace —
это имя + remote URL. Внутренний клон/кэш может существовать, но это деталь
реализации, а не пользовательский объект.

Что меняется:

- убрать `~/.also/sets/<name>` из пользовательской модели;
- убрать `SetConfig.Path` и `SetConfig.Source` из публичного config;
- убрать `--source` у `set`/`adopt`;
- `adopt` и `push` работают напрямую с remote namespace через внутренний кэш;
- `--into` остаётся только как явный «положить bundle сюда для отладки», но не
  как обязательный источник;
- `also set` заменить/дополнить естественной командой `also connect <name> <git-url>`;
- `also use <name>` остаётся выбором активного namespace.

Acceptance criteria:

- чистый `also setup` не создаёт `~/.also/sets`;
- `also adopt` без локального source-каталога пушит пакет в remote namespace;
- `also push <bundle-dir>` не требует локального set;
- старый config с `sets[].path/source` автоматически мигрирует;
- `also list/show/tree/install/upgrade` не показывают пользователю пути клонов;
- при недоступном remote ошибка говорит, какой namespace и URL недоступны.

Статус: реализовано. `SetConfig.Path/Source`, `--source`, `~/.also/sets` и
показ путей клонов убраны; config v1 мигрирует в v2 автоматически; `adopt`
без `--into` собирает временный бандл и пушит его в namespace.

### D2. Установка через curl + интерактивный setup

Пользователь не должен вручную собирать Go и прописывать config.

Поток:

```sh
curl -fsSL https://.../install.sh | sh
also setup
```

`install.sh`:

- определяет OS/arch;
- скачивает подписанный/проверенный по checksum бинарник;
- ставит в `~/.local/bin` (без sudo), проверяет `PATH`;
- не запускает ничего от root;
- поддерживает `ALSO_VERSION`, `ALSO_INSTALL_DIR`.

`also setup` (интерактивный, но пригодный для CI):

1. находит установленные harness CLI и их конфиги;
2. предлагает выбрать harness по умолчанию или ввести вручную;
3. спрашивает scope по умолчанию (`project`/`global`);
4. подключает один или несколько remote namespace (URL проверяется `git ls-remote`);
5. выбирает активный namespace;
6. опционально ставит стартовый пакет;
7. пишет config/state атомарно и печатает следующий шаг.

Non-interactive режим:

```sh
also setup --yes \
  --harness opencode \
  --namespace personal=git@github.com:me/ai-infra.git \
  --default-namespace personal
```

Acceptance criteria:

- `also setup` на пустом `ALSO_HOME` доводит до рабочего `also list`;
- повторный запуск идемпотентен;
- `--yes` не задаёт вопросы и возвращает понятный exit code;
- неверный URL/harness не оставляет полузаписанный config;
- установка работает на macOS/Linux, Windows — отдельным installer-путём.

Статус: `install.sh` и `install.ps1` добавлены, `also setup` и `--yes`
работают; проверено на macOS, Windows installer ещё не прогонялся.

## Блокеры 1.0

### V1-1. Дистрибуция и релиз

- [x] заменить `module also` на реальный import path;
- [x] добавить git remote и публичный репозиторий;
- [x] добавить `LICENSE`;
- [x] `Version = "1.0.0"` + commit/date в `also version`;
- [x] `.github/workflows`: gofmt, vet, test, race, Windows/macOS/Linux;
- [ ] release workflow: darwin/linux/windows amd64/arm64, checksums, подпись
      (сборка и checksums готовы, подпись — нет);
- [ ] install script и Homebrew/apt/winget — минимум один пакетный путь
      (install.sh/install.ps1 готовы, пакетного менеджера пока нет);
- [x] `CHANGELOG.md` и release notes.

### V1-2. Зафиксированные контракты и миграции

- [ ] объявить `also.pkg/v1` стабильным;
- [x] определить `also.config/v2` для remote-only namespaces;
- [ ] определить `also.state/v2` или явно закрепить обратную совместимость
      текущего `v1` (install slot id уже включает harness);
- [ ] версионировать формат community-адаптера (`adapter/v1`);
- [ ] написать migration-тесты: старый config/state/package → новый
      (config v1→v2 готов, state/package — остались);
- [ ] зафиксировать политику: что можно менять в patch/minor/major.

### V1-3. CI и тесты

- [ ] CLI coverage поднять с 8% до осмысленного уровня;
- [ ] тесты команд: install/push/adopt/upgrade/reshape/uninstall/setup/connect;
- [ ] golden-тесты вывода и exit codes;
- [ ] тесты natural-language parser и конфликтующих флагов;
- [ ] положить 4×4 E2E в `test/e2e` и запускать в CI;
- [ ] Windows CI: пути, CRLF, exec-bit, lock, имена пакетов;
- [ ] отдельный smoke с реальными harness CLI (без реальных моделей).

### V1-4. Trust/security

- [ ] описать threat model community-адаптеров;
- [ ] checksum/signature или pinning для `also add harness <url>`;
- [ ] подтверждение при подключении namespace/адаптера;
- [ ] расширить secret scanning: произвольные settings, URL, command args;
- [ ] политика `--allow-secrets`/`--allow-large` и redaction в логах;
- [ ] `SECURITY.md` с каналом сообщения об уязвимостях.

### V1-5. Диагностика и восстановление

- [ ] `also doctor`: git, config, state, adapters, dirty caches, PATH;
- [ ] `also verify`: целостность set, тегов, манифестов, state;
- [ ] recovery-команды для dirty clone, осиротевших тегов и битого state;
- [ ] понятные ошибки вместо ручных `git`-инструкций;
- [ ] тесты на corrupted config/state/manifest/tag.

### V1-6. Стабильный CLI/API

- [ ] JSON-вывод для install/push/adopt/upgrade/reshape/uninstall/setup;
- [ ] документированные exit codes;
- [ ] единая терминология `namespace` (убрать пользовательский `set`);
- [ ] command reference: natural form, flags, examples, JSON schema;
- [ ] `also version --json`;
- [ ] решить судьбу флагов: оставить как scripting API, natural form — основной.

## Порядок работ

1. **M0: repo hygiene** — import path, remote, LICENSE, CI, version metadata.
2. **M1: remote-only namespaces** — config v2, `connect`, убрать local set/source,
   миграция старого config, обновить adopt/push.
3. **M2: install + setup** — install script, `also setup`, non-interactive режим.
4. **M3: contracts + safety** — схемы, migration tests, adapter trust, secrets.
5. **M4: diagnostics + CLI contract** — doctor/verify, JSON, exit codes, docs.
6. **M5: 1.0 RC** — full CI matrix, E2E, release candidate, CHANGELOG.
7. **M6: 1.0 release** — signed binaries, packages, tag `v1.0.0`.

## После 1.0

- `also diff <pkg>@v1 v2`;
- global-scope `adopt`;
- nested `AGENTS.md`, inline agents/commands;
- больше адаптеров и codecs;
- shell completions;
- adapter update/versioning;
- stronger caching and faster `list`;
- `also status`/`also gc` для старых версий и тегов.

## Не входит в 1.0

- автоматический `gh repo create`;
- cross-harness permissions translation;
- поддержка symlink'ов в бандлах;
- произвольные новые codecs из JSON-адаптера (нужен Go-код и регистрация);
- полноценный GUI/TUI installer.

## Release gate 1.0

- [ ] install одной командой + `also setup` работают на чистой машине;
- [ ] remote-only namespace flow покрыт E2E;
- [ ] CI зелёный на macOS/Linux/Windows;
- [ ] старые config/state/package мигрируют без потери данных;
- [ ] 4×4 E2E 16/16 на релизном бинарнике;
- [ ] `also doctor` и `also verify` проходят на тестовом set;
- [ ] LICENSE, CHANGELOG, SECURITY, command reference готовы;
- [ ] бинарники подписаны, checksums опубликованы;
- [ ] tag `v1.0.0`.
