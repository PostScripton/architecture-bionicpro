# Keycloak vault

Эта директория монтируется в контейнер Keycloak как `--vault-dir` (провайдер `file`,
см. `docker-compose.yaml`). Она нужна, чтобы секрет Yandex ID (`clientSecret`)
не попадал в `keycloak/realm-export.json` в открытом виде - в файле указана
ссылка `${vault.yandex_client_secret}`, а сам секрет читается Keycloak'ом
из файла на диске в момент обращения к Яндексу.

Файлы в этой директории (кроме этого README) не хранятся в git - см. `.gitignore`.

Чтобы включить реальный вход через Yandex ID, создайте файл:

```
keycloak/vault/reports-realm_yandex__client__secret
```

Обратите внимание на двойное подчёркивание: провайдер `file` формирует имя файла
как `<realm>_<key>`, и при этом каждое подчёркивание внутри самого ключа
(`yandex_client_secret`) экранируется удвоением, чтобы не путать его с
разделителем `<realm>_<key>`. Поэтому `${vault.yandex_client_secret}` в
`realm-export.json` соответствует файлу `reports-realm_yandex__client__secret`,
а не `reports-realm_yandex_client_secret`.

Содержимое файла - Client Secret из личного кабинета https://oauth.yandex.ru/
(без переноса строки в конце, например
`printf '%s' 'ваш_секрет' > keycloak/vault/reports-realm_yandex__client__secret`).

`clientId` секретом не является (виден в URL при переходе на страницу
авторизации Яндекса) и указан открытым текстом прямо в `realm-export.json`.
