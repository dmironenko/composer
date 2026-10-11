# Секреты: SOPS + age

Composer расшифровывает SOPS YAML в `vars/base.yaml` и `vars/stage.yaml` перед
объединением настроек. Для шифрования и редактирования нужны CLI `sops` и `age`;
на сервере достаточно бинарника Composer.

## Ключ вне Git

Создайте age identity вне GitOps-репозитория:

```sh
mkdir -p "$HOME/.config/composer"
chmod 700 "$HOME/.config/composer"
age-keygen -o "$HOME/.config/composer/age.txt"
chmod 600 "$HOME/.config/composer/age.txt"
age-keygen -y "$HOME/.config/composer/age.txt"
```

Перенесите приватный ключ на сервер с правами `0600` для пользователя Composer
и укажите путь в host YAML:

```yaml
age_key_file: /etc/composer/age.txt
```

Путь разрешается от host YAML. Composer использует только этот файл;
`SOPS_AGE_KEY` и `SOPS_AGE_KEY_FILE` агент не читает. Для открытых vars ключ не нужен.

## Файлы в репозитории

В `.sops.yaml` GitOps-репозитория укажите публичного получателя из `age-keygen -y`:

```yaml
creation_rules:
  - path_regex: ^apps/[^/]+/[^/]+/vars/(base|stage)\.yaml$
    encrypted_regex: ^secrets$
    age: age1… # замените результатом age-keygen -y
```

Правило шифрует значения внутри `secrets`; без `encrypted_regex` шифруются все значения.
Имена ключей остаются видимыми. `.sops.yaml` хранится в Git. Блок `secrets`
располагайте внизу vars-файла, перед служебным блоком `sops`, который добавляет SOPS.

Пример `apps/demo/api/vars/base.yaml` до шифрования:

```yaml
app:
  listen: :8080
  database:
    host: db

compose:
  services:
    api:
      image: your-app:version
      command: /app/server --config /etc/app/config.yaml
      volumes: ["./configs/config.yaml:/etc/app/config.yaml:ro"]

secrets:
  database:
    password: replace-me
```

В `configs/config.yaml.tmpl` объедините настройки с секретами:

```gotemplate
{{ merge .Vars.app .Secrets | toYAML }}
```

Из корня GitOps-репозитория зашифруйте файл перед первым commit:

```sh
sops encrypt --in-place apps/demo/api/vars/base.yaml
```

Редактируйте через SOPS и открытые поля: MAC проверяет целостность всего документа.

```sh
SOPS_AGE_KEY_FILE="$HOME/.config/composer/age.txt" sops edit apps/demo/api/vars/base.yaml
```

Overrides шифруются так же. `stage.yaml` может содержать только изменяемое поле,
например `secrets.database.password`.

## Применение

После commit используйте обычные `validate`, `plan`, `apply` и `run`. Изменение
секрета в конфиге пересоздаёт контейнеры соответствующего проекта с прежним образом.
Повторное шифрование прежних значений не вызывает выкатку. Go templates выполняют
подстановки один раз; `$`, `$$` и `${…}` в значениях сохраняются буквально.

Открытые vars остаются в памяти, но готовые конфиги записываются в `state_dir`,
включая при `validate` и `plan`. Каталоги имеют права `0700`, конфиги имеют `0444`
(`0555` для исполняемых файлов). Монтируйте конфиги с `:ro` и защищайте резервные копии.
Старые версии сохраняются до удаления проекта. Compose environment доступен
пользователям с доступом к Docker.

Поддерживаются native age identities, несколько получателей в одной SOPS key group
и несколько identities в key file. KMS, PGP, SSH identities, age plugins и Shamir
key groups не поддерживаются. Ошибка ключа или MAC сохраняет текущий проект
и возвращает ошибку без содержимого секретов.

Библиотека [SOPS](https://github.com/getsops/sops/tree/v3.13.3) читает
`/etc/sops/audit.yaml` при запуске. Ошибка настроенного audit backend может
остановить Composer; отдельного выключателя audit нет. Формат и команды описаны
в [документации SOPS](https://sops.pages.dev/).
