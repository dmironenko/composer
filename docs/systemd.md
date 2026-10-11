# Запуск на Ubuntu

Установите Docker Engine и Linux-бинарник Composer. Разместите host YAML, ключи
и локальный GitOps-репозиторий (для `source.mode: local`) по постоянным путям.
Относительные пути в конфиге разрешаются от YAML.

```sh
sudo ./composer install --config /etc/composer/host.yaml
```

Для `installation: demo` команда проверяет конфиг, копирует бинарник в
`/usr/local/lib/composer/demo/composer`, создаёт
`/etc/systemd/system/composer-demo.service` и запускает агент с автозапуском.
Конфиг остаётся на месте; unit ссылается на его абсолютный путь.

Команда возвращается после запуска процесса. Ход выкатки смотрите в журнале:

```sh
sudo systemctl status composer-demo
sudo journalctl -u composer-demo -f
```

Агент работает от root с его Docker config/context. Для локального rootful Docker
можно задать `docker.host: unix:///var/run/docker.sock`. Переменные окружения
оболочки в unit не копируются; credentials и age key должны быть доступны агенту.

## Обновление и остановка

Повторный `install` обновляет бинарник и unit и перезапускает агент этой installation.
Чужой unit команда не перезаписывает. Дополнительные настройки задавайте через
`systemctl edit composer-demo`.

```sh
sudo ./composer-new install --config /etc/composer/host.yaml
sudo systemctl stop composer-demo
sudo systemctl start composer-demo
```

Остановка агента сохраняет контейнеры. При старте с прежним state_dir успешно
применённые проекты без изменений пропускаются. После сбоя systemd перезапускает
агент через 5 секунд; после перезагрузки сервера он стартует автоматически.

Чтобы убрать автозапуск, сохранив приложения:

```sh
sudo systemctl disable --now composer-demo
```

Для удаления сервиса удалите его unit и каталог бинарника, затем выполните
`systemctl daemon-reload`. State dir и Docker-ресурсы сохраняются.

## Проверка на настоящем systemd

Запускайте [test/systemd/check.sh](../test/systemd/check.sh) от root в одноразовой
Ubuntu VM с Docker, Git и Python 3. Скопируйте скрипт в `/var/tmp/systemd-check.sh`,
а бинарник в `/tmp/composer`:

```sh
sudo bash /var/tmp/systemd-check.sh /tmp/composer
# Сценарий напечатает каталог /var/tmp/composer-systemd.<suffix>.
sudo reboot
# После повторного подключения:
sudo bash /var/tmp/systemd-check.sh reboot-check /var/tmp/composer-systemd.<suffix>
```

Сценарий проверяет install, обновление, stop/start, перезапуск после сбоя,
config-only изменения, сохранение чужого unit и автозапуск после reboot с прежним
контейнером. После проверки удалите VM.
