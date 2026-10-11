# Сборка приложений из Git

Composer получает исходники указанного commit, рендерит `Dockerfile.tmpl` из
GitOps-репозитория и собирает образ в Docker. Образы и build cache хранит Docker;
Composer не создаёт собственное хранилище и не удаляет образы автоматически.

## Файлы приложения

```text
apps/<namespace>/<app>/
  Dockerfile.tmpl
  vars/
    base.yaml
    stage.yaml       # необязательно: включает stage
  configs/
    config.yaml.tmpl
```

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

URL и SHA в примере замените своими. `build.commit` — полный SHA из 40 строчных
hex-символов; branch, tag и короткий SHA не принимаются. `build.repository`
поддерживает HTTPS и SSH, включая `git@host:org/repo.git`.

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

Dockerfile получает те же Go template vars, включая `.Vars.build.repository`
и `.Vars.build.commit`. Аргумент сборки `COMPOSER_COMMIT` Composer передаёт
автоматически; чтобы использовать его в Dockerfile, объявите `ARG` в нужном stage.
Произвольной карты build args в текущем контракте нет.

Composer уже делает checkout до сборки. Build context — дерево исходников
именно этого SHA; `.git` удаляется. Dockerfile не должен повторно клонировать
репозиторий или выполнять `git checkout`. Сам `Dockerfile.tmpl` берётся из
GitOps-репозитория и передаётся builder отдельно от исходников.

## Образы, обновление и откат

Тег `composer/build:<sha256>` зависит от трёх входов: repository, commit и
содержимого отрендеренного Dockerfile. Для одинаковых входов prod и stage
используют один образ. Общий Compose template подставляет этот тег и
`pull_policy: never` только сервисам без собственного `image`; sidecars с
явным image сохраняются. При нескольких сервисах без image все получат один
собранный образ, с собственными command и остальными настройками.

При переопределении общего template используйте `.Runtime.Image` и задайте
`pull_policy: never` для локально собираемого сервиса. Старую копию общего
шаблона нужно обновить самостоятельно: экспорт нового template не меняет
существующий GitOps-репозиторий автоматически.

Перед применением изменённого project Composer проверяет image в Docker.
Найденный образ используется без checkout и сборки. При отсутствии образа
исходники выгружаются во временный закрытый каталог, затем удаляются после
завершения операции. Одновременные запросы одного тега объединяются только
на время сборки; завершённые результаты Composer не кэширует.

Образ должен быть готов до удаления прежнего applied marker и до Compose Up.
Ошибка checkout или сборки сохраняет работающий project и его прежний hash
успеха. После успешной сборки действуют обычные правила выкатки: частичная
ошибка Up инвалидирует прежний hash, поэтому Git revert не будет пропущен.

Runtime config не входит в image hash. Config-only изменение пересоздаёт
контейнер с прежним image; если изменённая переменная используется также в
`Dockerfile.tmpl`, меняется Dockerfile и потребуется другой образ.
Revert возвращает прежние входы и использует сохранившийся image. Если его
удалили из Docker, Composer соберёт его заново при следующем применении project.
Неизменённые успешные projects по-прежнему пропускаются; отдельного image
или container drift polling нет.

Base image с изменяемым tag не вызывает автоматическую пересборку уже готового
образа. Для строгой воспроизводимости закрепляйте base images по digest и
контролируйте остальные загружаемые во время Dockerfile зависимости.

## Stage и выпуск

Для проверки другого SHA задайте в `vars/stage.yaml`:

```yaml
build:
  commit: "89abcdef0123456789abcdef0123456789abcdef01"
```

Repository и остальное наследуются из base. При выпуске перенесите проверенный
SHA в `base.yaml`, удалите override commit из stage, но оставьте `stage.yaml`
хотя бы с `{}`. Тогда оба окружения используют base SHA. Пока stage override
существует, изменение base commit не обновляет stage на этот commit.

## Доступ к приватным исходникам

`source.auth` отвечает за GitOps-репозиторий. Для исходников приложений есть
отдельная общая настройка `build_auth` в host YAML; она применяется ко всем
build repositories этой installation.

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

Пути разрешаются относительно host YAML. Ключи и token хранятся вне Git.
Для публичного HTTPS repository `build_auth` можно не задавать. Для SSH нужны
явно настроенные ключ и known_hosts; ambient SSH agent не используется.
Git server должен разрешать fetch указанного SHA. Submodules отклоняются;
Git LFS объекты не загружаются, в context останутся pointer files.

## Runtime и проверки

Build adapter обращается к встроенному BuildKit Docker Engine через `/grpc`
и `/session`, экспортируя результат в локальное хранилище Docker (`moby`).
Для checkout и сборки Composer не вызывает внешние `git`, `ssh` или `buildx`.
Docker daemon должен предоставлять эти BuildKit endpoints.

Registry credentials берутся из Docker config. Если в нём настроен внешний
credential helper, его executable остаётся необходим для соответствующего
registry. Команды `RUN` внутри Dockerfile и зависимости самого приложения
определяются содержимым образа, а не Composer.

`validate`/`plan` проверяют build vars и рендерят Dockerfile, но не обращаются
к source repository приложения или Docker и не доказывают успешность сборки.
Сам build выполняется в apply/run внутри worker и общего deploy timeout;
для долгих сборок увеличьте `reconcile.deploy_timeout` и timeout команды apply.

Тесты checkout и embedded BuildKit запускаются отдельно от обычных unit-тестов:

```sh
go test -tags=integration ./internal/app/composer/adapters/git -count=1
go test -tags=integration ./internal/app/composer/adapters/docker -count=1
go test -race -tags=integration ./cmd/composer -count=1
```

При `source.branches: {prod: master, stage: dev}` меняйте исходный `build.commit`
и `Dockerfile.tmpl` в dev. Merge в master переносит проверенную сборку в prod;
Docker переиспользует образ, если repository, commit и rendered Dockerfile совпали.
Для такой схемы не задавайте отдельный `build.commit` в stage.yaml без необходимости:
после merge override продолжит действовать и stage может расходиться с prod.
