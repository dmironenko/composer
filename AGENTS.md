# Composer

Перед изменениями проверь git status и сохрани работу пользователя.

## Главный принцип

Минимум кода: Git → vars/templates → сети → Compose SDK. Composer знает
соглашение `apps/<namespace>/<name>` и окружения, но не типы приложений.
NGINX, HAProxy, БД и другие сервисы описываются только в GitOps-репозитории.
Не добавляй специальные Go-ветки, собственный контроллер контейнеров,
машину состояний, framework-слои и дублирующие Compose ограничения.
Сокращай ответственность, а не сжимай строки.

Сначала ищи готовое решение в стандартной библиотеке или зависимостях.
Сохраняй поведение, делай минимальные изменения и проверяй их существующими тестами.
Не добавляй абстракции без нескольких реальных потребителей.

## Контракты

- В YAML-примерах отступ — два пробела; command и healthcheck.test — однострочные строки.
  Для shell-команд явно указывай `sh -c`; перед блоками compose и secrets — пустая строка.
- Prod = base; любое другое окружение = base + stage. Основные настройки — base.yaml.
- Нет stage.yaml — приложение выключено во всех non-prod окружениях; пустой файл включает его.
- Единственный список окружений — source.branches; имена произвольные, prod — особый случай.
- Run перечитывает host YAML каждый проход; ошибочный конфиг сохраняет последние принятые настройки.
- Удаление записи окружения удаляет его контейнеры, включая после restart; volumes/images сохраняются.
  В наших примерах prod=master, stage=dev; у клиента любые имена.
- Ошибка source/render одного окружения не вызывает его удаления и не блокирует другое.
- Один каталог — независимый Compose project; namespace/environment — отдельная сеть.
- Все namespace networks, включая запрошенные через helper network, готовятся до параллельных Compose operations.
- Config-only update действительно применяется; scale не пересоздаёт прежние реплики.
- SOPS vars расшифровываются до merge; age_key_file вне Git, plaintext не выводится в отчётах.
- Подстановки — только Go templates; второй Compose interpolation отключён для сохранения `$` в секретах.
- После неудачного применения старый хэш успеха не должен скрывать Git revert.
- Volumes/images не удаляются автоматически; GitOps repo считается доверенным.
- Образы и build cache хранит Docker; отдельный менеджер кэша Composer не нужен.
- Source image определяется repository + полный commit SHA + отрендеренный Dockerfile.
- Build завершается до инвалидирования applied marker и Up; ошибка build сохраняет прежний project.
- Исходники читаются через build_auth отдельно от source.auth; credentials не входят в GitOps.
- Runtime build использует embedded BuildKit и проверяется adapter integration tests.
- GitHub Actions не требуется: Composer сам читает Git, проверяет и применяет изменения.
- Код не знает demo-приложений. Init создаёт пустую структуру, fixtures живут в тестах.

## Где смотреть

[README](README.md) — работающие команды и ограничения.
[Архитектура](docs/architecture.md) — контракт и границы кода.
[Сборки](docs/builds.md) — Dockerfile, commit, auth и границы runtime.
[Systemd](docs/systemd.md) — установка, обновление и проверка в Ubuntu VM.
Не представляй будущие возможности как реализованные.

Следуй idiomatic Go и инструкциям [CONTRIBUTING.md](CONTRIBUTING.md).
Гексагональные границы (`internal/app/composer`):

- `domain` — модели, значения и чистые правила; только stdlib без IO, без SDK и DTO-тегов.
- `service/{reconcile,deployment,build}` — сценарии, политика и consumer-owned
  исходящие интерфейсы в ports.go. Нет imports config, os, SDK, ports или adapters.
- `ports/cli` — входные команды, flags и JSON DTO; не создаёт внешние клиенты.
- `adapters/{git,gitops,docker,state,systemd}` — технические реализации портов.
  Адаптер не импортирует соседний адаптер или inbound port.
- `cmd/composer` — единственный composition root: host YAML, bootstrap, lock,
  конструкторы и cleanup. Политика polling/retry принадлежит reconcile.

Compose SDK model существует только внутри GitOps/Docker adapters. Между ними
передаётся domain.Manifest с исходным rendered document, WorkingDir и ConfigHash;
service не разбирает содержимое. Hash считается по прежнему normalized model.
Нельзя прятать SDK в any, type alias или callback внутри domain.

Файловые markers и временные build directories обслуживает adapters/state.
Service решает порядок операций через Inventory/Journal/Workspace; не выполняет
IO самостоятельно. CLI маппит domain.Report в публичный DTO без manifest/secrets.
Не добавляй DI framework, общий registry или дублирующую Compose schema.

Границы обязательны и для тестов. Полные wiring/integration tests живут в cmd;
service tests используют consumer-port mocks. Mocks генерируются закреплённым
mockgen через `make go/generate`.

[composer-example](https://github.com/dmironenko/composer-example) — отдельный GitOps-пример.

## Проверки

Команды находятся в CONTRIBUTING.md. Для кода нужны build/test/vet/lint, для goroutines — race.
Используй линтер и конфигурацию platform напрямую через `make go/lint`.
Не создавай локальную копию `.golangci.yml` и не переопределяй платформенные правила.
Перед первым запуском и после изменения зависимостей — `make go/vendor`;
сгенерированный `vendor/` не хранится в Git.
`make go/architecture` использует checker platform; `make go/lint` запускает его перед lint.
Проверяй также integration-файлы платформенным линтером с `--build-tags=integration`
и `--config "$PLATFORM_DIR/configs/go/golangci.yml"` (полная команда — в CONTRIBUTING.md).
Docker tests создают только собственные projects/networks и сохраняют чужие ресурсы.
Systemd проверяется в одноразовой Ubuntu VM через `test/systemd/check.sh`, включая reboot.
`composer-example` содержит только runnable GitOps-пример; тестовая инфраструктура в нём не хранится.
