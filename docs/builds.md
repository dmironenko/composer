# Сборка приложений из Git

Версия приложения задаётся в GitOps-репозитории. Его `Dockerfile.tmpl` получает
исходники и компилирует приложение; Composer запускает сборку через Docker.
Образы и кэш сборки хранит Docker; автоматического удаления нет.

## Файлы приложения

В `vars/base.yaml`:

```yaml
build:
  repository: https://github.com/example/api.git
  tag: v1.0.0

compose:
  services:
    api:
      command: /app/api --config /etc/app/config.yaml
      volumes: [./configs/config.yaml:/etc/app/config.yaml:ro]
      deploy:
        replicas: 2
app:
  listen: :8080
```

Замените URL и tag своими. `build.tag` принимает SHA, тег или ветку;
Git разрешает указанную ссылку внутри Dockerfile. `build.repository` поддерживает HTTPS и SSH, включая `git@host:org/repo.git`.

Пример `Dockerfile.tmpl` для Go-приложения с main package в корне исходников:

```dockerfile
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
RUN git clone {{ quote .Vars.build.repository }} . \
    && git checkout {{ quote .Vars.build.tag }} \
    && CGO_ENABLED=0 go build -o /out/api .

FROM scratch
LABEL org.opencontainers.image.revision={{ quote .Vars.build.tag }}
COPY --from=builder /out/api /app/api
```

`configs/config.yaml.tmpl` может целиком копировать настройки:

```gotemplate
{{ .Vars.app | toYAML }}
```

Dockerfile получает Go template vars, включая `.Vars.build.repository` и
`.Vars.build.tag`. Оба значения подставляются при рендеринге Dockerfile.
Composer не передаёт build args.

Build context пустой. Исходники загружает сам Dockerfile; локальный `COPY . .`
не передаёт файлы из GitOps или с сервера. Для multi-stage сборки используйте
`COPY --from=builder`.

## Образы, обновление и откат

Тег `composer/build:<sha256>` зависит от repository, tag и отрендеренного
Dockerfile. Если образ уже есть в Docker, Composer использует его без checkout
и сборки. Иначе выполняет Dockerfile.
Одновременные сборки одного тега объединяются.

Composer отслеживает GitOps-репозиторий, а не перемещения refs исходного приложения.
Смена `build.tag` с `v1.0.0` на `v1.1.0` вызывает сборку. Перемещение прежнего тега
или ветки не вызывает обновление и не сбрасывает кэш Docker. Для воспроизводимых
выкаток используйте SHA или теги, которые не перемещаются.

Общий template подставляет образ и `pull_policy: never` сервисам без своего `image`.
Несколько таких сервисов получают один образ. В собственном template используйте
`.Runtime.Image` и `pull_policy: never`; обновляйте скопированный template вручную.

Ошибка checkout или сборки сохраняет работающий проект. Изменение конфига
пересоздаёт контейнер с прежним образом, если переменные не изменили Dockerfile.
При revert Composer использует сохранившийся образ или собирает его заново.
Успешно применённые проекты без изменений пропускаются.

Изменение базового образа за тем же тегом не вызывает пересборку. Закрепляйте
версии базовых образов и зависимостей, загружаемых при сборке.

## Stage и выпуск

При `source.branches: {prod: master, stage: dev}` меняйте `build.tag` в
`base.yaml` и `Dockerfile.tmpl` на dev. Merge в master переносит сборку в prod;
при одинаковых входах Docker использует тот же образ.

Отдельную версию для всех non-prod окружений можно задать в `vars/stage.yaml`:

```yaml
build:
  tag: 89abcdef0123456789abcdef0123456789abcdef01
```

Этот override продолжает действовать после merge. Чтобы вернуть общую версию,
перенесите его в `base.yaml` и удалите override, сохранив `stage.yaml` хотя бы с `{}`.

В старых Dockerfile замените `$BUILD_TAG` на `{{ quote .Vars.build.tag }}`
и удалите `ARG BUILD_TAG`.

## Доступ к исходникам

Пример использует публичный HTTPS. `source.auth` относится только к GitOps-репозиторию.
Composer не передаёт ключи, SSH agent или токены внутрь Dockerfile и не предоставляет
BuildKit secret/SSH mounts. Приватный clone с такой авторизацией пока не поддерживается.
Не подставляйте credentials в Dockerfile через vars: они попадут в инструкции сборки.

При переходе со старого механизма удалите `build_auth` из host YAML и перенесите
checkout из CLI в Dockerfile, как в примере выше.

## Сборка и проверка

Docker Engine должен предоставлять embedded BuildKit endpoints `/grpc` и `/session`.
Composer обращается к BuildKit через SDK; `git` для clone устанавливается в builder stage.
Registry credentials читаются из Docker config; настроенный credential helper
должен быть установлен.

`validate` и `plan` проверяют vars и рендерят Dockerfile без обращения к исходникам
или Docker. Сборка выполняется в `apply`/`run`. Для долгих сборок увеличьте
`reconcile.deploy_timeout` и timeout команды apply. Команды integration-тестов
приведены в [CONTRIBUTING.md](../CONTRIBUTING.md).
