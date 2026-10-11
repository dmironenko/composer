# Сборка приложений из Git

Composer получает исходники указанного commit и собирает образ по `Dockerfile.tmpl`
из GitOps-репозитория. Образы и кэш сборки хранит Docker; автоматического удаления нет.

## Файлы приложения

В `vars/base.yaml`:

```yaml
build:
  repository: https://github.com/example/api.git
  commit: "0123456789abcdef0123456789abcdef01234567"

compose:
  services:
    api:
      command: /app/api --config /etc/app/config.yaml
      volumes: ["./configs/config.yaml:/etc/app/config.yaml:ro"]
      deploy:
        replicas: 2
app:
  listen: ":8080"
```

Замените URL и SHA своими. `build.commit` принимает полный SHA из 40 строчных
hex-символов. `build.repository` поддерживает HTTPS и SSH, включая `git@host:org/repo.git`.

Пример `Dockerfile.tmpl` для Go-приложения с main package в корне исходников:

```dockerfile
FROM golang:1.26 AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api .

FROM scratch
ARG COMPOSER_COMMIT
LABEL org.opencontainers.image.revision=$COMPOSER_COMMIT
COPY --from=builder /out/api /app/api
```

`configs/config.yaml.tmpl` может целиком копировать настройки:

```gotemplate
{{ .Vars.app | toYAML }}
```

Dockerfile получает Go template vars, включая `.Vars.build.repository` и
`.Vars.build.commit`. Composer передаёт build arg `COMPOSER_COMMIT`; объявите
`ARG` в нужном stage, чтобы использовать его. Другие build args не поддерживаются.

Build context содержит дерево исходников выбранного SHA без `.git`.
Composer выполняет checkout до сборки и передаёт Dockerfile отдельно.

## Образы, обновление и откат

Тег `composer/build:<sha256>` зависит от repository, commit и отрендеренного
Dockerfile. Если образ уже есть в Docker, Composer использует его без checkout
и сборки. Иначе собирает из временного каталога и удаляет каталог после операции.
Одновременные сборки одного тега объединяются.

Общий template подставляет образ и `pull_policy: never` сервисам без своего `image`.
Несколько таких сервисов получают один образ. В собственном template используйте
`.Runtime.Image` и `pull_policy: never`; обновляйте скопированный template вручную.

Ошибка checkout или сборки сохраняет работающий проект. Изменение конфига
пересоздаёт контейнер с прежним образом, если переменные не изменили Dockerfile.
При revert Composer использует сохранившийся образ или собирает его заново.
Успешно применённые проекты без изменений пропускаются.

Изменение базового образа за тем же тегом не вызывает пересборку. Для воспроизводимости
закрепляйте базовые образы по digest и версии зависимостей, загружаемых при сборке.

## Stage и выпуск

При `source.branches: {prod: master, stage: dev}` меняйте `build.commit` в
`base.yaml` и `Dockerfile.tmpl` на dev. Merge в master переносит сборку в prod;
при одинаковых входах Docker использует тот же образ.

Отдельный SHA для всех non-prod окружений можно задать в `vars/stage.yaml`:

```yaml
build:
  commit: "89abcdef0123456789abcdef0123456789abcdef01"
```

Этот override продолжает действовать после merge. Чтобы вернуть общий SHA,
перенесите его в `base.yaml` и удалите override, сохранив `stage.yaml` хотя бы с `{}`.

## Доступ к приватным исходникам

`source.auth` задаёт доступ к GitOps-репозиторию, а `build_auth` в host YAML
задаёт доступ ко всем репозиториям исходников этой installation.

SSH:

```yaml
build_auth:
  type: ssh
  key_file: keys/build_ed25519
  known_hosts_file: keys/known_hosts
```

HTTPS:

```yaml
build_auth:
  type: https
  token_file: keys/build_token
```

Пути разрешаются относительно host YAML; ключи и токен хранятся вне Git.
Для публичного HTTPS `build_auth` не нужен. SSH требует явных key и known_hosts;
SSH agent не используется. Git-сервер должен разрешать fetch указанного SHA.
Submodules не поддерживаются; вместо объектов Git LFS останутся файлы-указатели.

## Сборка и проверка

Docker Engine должен предоставлять embedded BuildKit endpoints `/grpc` и `/session`.
Composer использует go-git и BuildKit без внешних `git`, `ssh` и `buildx`.
Registry credentials читаются из Docker config; настроенный credential helper
должен быть установлен.

`validate` и `plan` проверяют vars и рендерят Dockerfile без обращения к исходникам
или Docker. Сборка выполняется в `apply`/`run`. Для долгих сборок увеличьте
`reconcile.deploy_timeout` и timeout команды apply. Команды integration-тестов
приведены в [CONTRIBUTING.md](../CONTRIBUTING.md).
