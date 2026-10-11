# Секреты: SOPS + age

Composer читает обычные и зашифрованные SOPS YAML в `vars/base.yaml`,
`vars/stage.yaml`. Каждый файл расшифровывается перед объединением:
prod = base, остальные окружения = base + stage. Основные настройки остаются в base.

## Ключ вне Git

Создайте age identity вне GitOps-репозитория. Команды ниже выполняются один раз;
на сервер перенесите приватный ключ с правами `0600` для пользователя Composer.

```sh
mkdir -p "$HOME/.config/composer"
chmod 700 "$HOME/.config/composer"
age-keygen -o "$HOME/.config/composer/age.txt"
chmod 600 "$HOME/.config/composer/age.txt"
age-keygen -y "$HOME/.config/composer/age.txt"
```

Последняя команда выводит публичного получателя `age1…`. В host YAML Composer
добавьте путь к приватному ключу:

```yaml
age_key_file: /etc/composer/age.txt
```

Относительный путь разрешается от host YAML. Для незашифрованных vars этот ключ
не требуется. Для расшифровки Composer использует только указанный файл;
`SOPS_AGE_KEY`, `SOPS_AGE_KEY_FILE` и внешние команды поиска ключей не используются.

## Файлы в репозитории

В корневом `.sops.yaml` GitOps-репозитория задайте публичного получателя:

```yaml
creation_rules:
  - path_regex: ^apps/[^/]+/[^/]+/vars/(base|stage)\.yaml$
    encrypted_regex: ^app$
    age: age1… # замените результатом age-keygen -y
```

Это правило шифрует значения внутри `app`, оставляя остальной YAML читаемым.
Чтобы шифровать все значения, уберите `encrypted_regex`. Имена YAML-ключей
остаются видимыми. `.sops.yaml` содержит публичные настройки и хранится в Git.

Пример `apps/demo/api/vars/base.yaml` до шифрования:

```yaml
app:
  listen: :8080
  database:
    host: db
    password: replace-me

compose:
  services:
    api:
      image: your-app:version
      command: /app/server --config /etc/app/config.yaml
      volumes: ["./configs/config.yaml:/etc/app/config.yaml:ro"]
```

В `configs/config.yaml.tmpl` достаточно одной строки:

```gotemplate
{{ .Vars.app | toYAML }}
```

Из корня GitOps-репозитория зашифруйте файл перед первым commit:

```sh
sops encrypt --in-place apps/demo/api/vars/base.yaml
```

Последующие изменения вносите через SOPS, включая открытые поля: MAC проверяет
целостность документа.

```sh
SOPS_AGE_KEY_FILE="$HOME/.config/composer/age.txt" sops edit apps/demo/api/vars/base.yaml
```

Overrides шифруются аналогично. Перечислять все настройки заново не нужно:
например, `stage.yaml` может содержать только `app.database.password`.
Для создания и редактирования файлов нужны CLI `sops` и `age`; на сервере
Composer расшифровывает их внутри Go-бинарника без этих CLI.

## Применение

После commit работают обычные `validate`, `plan`, `apply` и `run`. Изменение
секрета, использованного в конфиге, меняет хэш отрендеренного конфига и
пересоздаёт контейнеры соответствующего project с прежним образом.
Соседние projects не меняются. Повторное шифрование без изменения значений
не вызывает выкатку. Revert возвращает прежнее значение обычным применением.

Go templates выполняют подстановки один раз. Второй проход Compose interpolation
отключён: `$`, `$$` и `${…}` в значениях остаются буквальными. Для shell expansion
в `command: [sh, -c, …]` используйте `$VAR`, без удвоения доллара.

Plaintext vars остаются в памяти; Git snapshots сохраняют шифротекст. Однако
готовые configs записываются в `state_dir`, в том числе командами `validate`
и `plan`. Родительские каталоги закрыты правами `0700`, файлы имеют `0444`
(исполняемые — `0555`) для чтения процессом внутри контейнера. Монтируйте configs
с `:ro`. Старые версии configs сохраняются до удаления project. Значения,
переданные в Compose environment, доступны пользователям с доступом к Docker.

Поддерживаются native age identities, несколько получателей в одной SOPS key
group и несколько identities в key file. KMS, PGP, SSH identities, age plugins
и Shamir key groups не поддерживаются. Ошибка ключа или MAC оставляет текущий
project запущенным и возвращает ошибку без содержимого секретов.

Используется библиотека [SOPS](https://github.com/getsops/sops/tree/v3.13.3).
Её встроенный audit при наличии `/etc/sops/audit.yaml` инициализируется до запуска
CLI; отдельного выключателя в Composer нет. Ошибка такого audit backend может
остановить процесс. Подробности формата и редактирования — в
[документации SOPS](https://sops.pages.dev/).
