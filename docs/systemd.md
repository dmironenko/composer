# Запуск на Ubuntu

На сервере должны быть установлены Docker Engine и Linux-бинарник Composer.
Host YAML, ключи и local GitOps repo (если используется `source.mode: local`)
нужно разместить по постоянным путям. Относительные пути в YAML разрешаются
относительно самого YAML, как при foreground-запуске.

```sh
sudo ./composer install --config /etc/composer/host.yaml
```

Для `installation: demo` команда:

1. Проверит host YAML.
2. Скопирует текущий бинарник в `/usr/local/lib/composer/demo/composer`.
3. Запишет `/etc/systemd/system/composer-demo.service`.
4. Выполнит `daemon-reload`, включит автозапуск и запустит агент.

Конфиг остаётся по исходному пути; unit ссылается на его абсолютный путь.
Не удаляйте YAML после установки. Исходный скачанный бинарник можно удалить.
Команда завершается после запуска процесса; ход выкатки виден в журнале:

```sh
sudo systemctl status composer-demo
sudo journalctl -u composer-demo -f
```

Агент работает от root и использует Docker config/context этого пользователя.
Для локального rootful Docker можно явно задать
`docker.host: unix:///var/run/docker.sock` в host YAML.
Переменные окружения вызывающей оболочки в unit не копируются.
Docker, Git credentials, SOPS age key и содержимое GitOps управляются отдельно.

## Обновление и остановка

Повторный `install` обновляет бинарник и unit, затем перезапускает только агент
этой installation. Файлы заменяются через rename, поэтому можно обновляться
даже из установленного бинарника. Unit, созданный не через Composer, команда
не перезаписывает. Дополнительные настройки systemd задавайте через
`systemctl edit composer-demo`.

```sh
sudo ./composer-new install --config /etc/composer/host.yaml
sudo systemctl stop composer-demo
sudo systemctl start composer-demo
```

Остановка агента сохраняет контейнеры. При старте агент читает тот же state_dir,
поэтому неизменившиеся успешно применённые projects не пересоздаются.
После аварийного завершения systemd перезапускает агент через 5 секунд.
После перезагрузки сервера агент запускается автоматически.

Чтобы убрать автозапуск, сохранив приложения:

```sh
sudo systemctl disable --now composer-demo
```

При необходимости затем удалите только его unit и каталог бинарника и выполните
`systemctl daemon-reload`. State dir, Docker images, volumes и контейнеры
автоматически не удаляются. На macOS используется `composer run --config …`.

## Проверка на настоящем systemd

Сценарий [test/systemd/check.sh](../test/systemd/check.sh) запускается **в отдельной
одноразовой Ubuntu VM** от root. Нужны Docker, Git и Python 3. Он создаёт свои
repo, unit и HTTP-контейнер без публикации host ports и оставляет их для проверки
после перезагрузки.

```sh
sudo bash /var/tmp/systemd-check.sh /tmp/composer
# Сценарий напечатает каталог /var/tmp/composer-systemd.<suffix>.
sudo reboot
# После повторного подключения:
sudo bash /var/tmp/systemd-check.sh reboot-check /var/tmp/composer-systemd.<suffix>
```

Проверяются install, экранирование пути YAML, повторная установка, stop/start,
перезапуск после SIGKILL, config-only commit, сохранение чужого unit и автозапуск
после реальной перезагрузки с прежним ID контейнера и ожидаемым HTTP-ответом.
После проверки удалите тестовую VM.

Формат unit и экранирование аргументов следуют
[systemd.service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).
