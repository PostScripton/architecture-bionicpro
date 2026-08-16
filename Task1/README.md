# Задание 1. Повышение безопасности системы

## Архитектурное решение

<img src="/images/tasks/Task1/bionicpro_credential_management.svg" alt="Диаграмма управления учётными данными BionicPRO"/>

Ключевые решения:

- Единая точка аутентификации - Keycloak (realm `reports-realm`). Представительства BionicPRO в других странах подключаются к нему через собственный LDAP-сервер (Identity Federation), персональные и медицинские данные при этом физически остаются в стране представительства - Keycloak только читает учётные записи и роли по сети, но не копирует персональные данные к себе.
- Поддержка внешних удостоверяющих служб реализована через Identity Brokering (Яндекс ID), что позволяет добавлять новых внешних IdP разных стран без изменения кода приложения.
- Токены access/refresh, полученные от Keycloak, никогда не передаются во фронтенд. Обмен кода авторизации на токены и вся дальнейшая работа с ними вынесены в новый бэкенд-сервис `bionicpro-auth`. Фронтенд получает только сессионную cookie.

## PKCE вместо Code Grant

- Клиент `reports-frontend` в Keycloak переведён на Authorization Code Flow с PKCE (`pkce.code.challenge.method: S256`), `directAccessGrantsEnabled` отключён.
- `code_verifier` генерируется и хранится только на бэкенде (`bionicpro-auth`), никогда не покидает сервер - это исключает перехват кода авторизации на фронтенде.

## Сервис bionicpro-auth

Новый сервис `bionicpro-auth/` (Go) реализует:

- `GET /auth/login` - генерирует PKCE code_verifier/code_challenge и state, редиректит браузер на Keycloak.
- `GET /auth/callback` - обменивает code + code_verifier на access/refresh токены (запрос сервер-сервер, без секрета клиента), создаёт сессию.
- Токены хранятся в памяти сервиса в зашифрованном виде (AES-256-GCM, ключ шифрования генерируется при старте процесса и никогда не покидает память) - хранилище спроектировано так, чтобы его можно было заменить на распределённый кеш без изменения остальной логики.
- Фронтенду в ответ выдаётся только сессионная cookie с флагами `HttpOnly` и `Secure` (в docker-compose для локальной разработки по http флаг `Secure` выключен через `COOKIE_SECURE=false` - в проде вместе с TLS-терминацией на Nginx он обязателен).
- Время жизни access_token в Keycloak установлено 120 секунд, время жизни сессии больше (по умолчанию 900 секунд) - когда access_token истекает, `bionicpro-auth` прозрачно обновляет его через refresh_token.
- При каждом запросе к защищённому ресурсу (`GET /reports`, `GET /auth/session`) сервис перепривязывает access/refresh токены к новому session id, обновляет cookie и возвращает новый id в заголовке `X-Session-Id` - это защита от session fixation.
- `GET /reports` работает как прокси к будущему `reports-api` с подстановкой `Authorization: Bearer` на бэкенде; пока `reports-api` не поднят (реализуется в задании 2), возвращает заглушку, подтверждающую успешную аутентификацию.

Фронтенд обновлён: убран `keycloak-js`/`@react-keycloak/web`, вход теперь выполняется переходом на `/auth/login` бэкенда, все запросы к API идут с `credentials: 'include'`.

## LDAP-федерация

- `ldap/` - образ OpenLDAP (`osixia/openldap`) с бутстрап-данными из `config.ldif`, поднимается сервисом `ldap` в `docker-compose.yaml`.
- В Keycloak настроен LDAP User Storage Provider (`bionicpro-ldap-foreign-office`) с маппером ролей `role-ldap-mapper`, синхронизирующим группы `ou=Groups` в роли realm (`user`, `prothetic_user`), и маппером атрибутов username/email/firstName/lastName.
- Проверено полной синхронизацией: пользователи `john.doe`, `jane.smith`, `alex.johnson` импортируются из LDAP с ролью `prothetic_user`/`user`, данные при этом продолжают физически храниться в LDAP, а не копируются в основную БД Keycloak.

## MFA

- Включена политика TOTP на уровне realm, всем пользователям назначено обязательное действие `CONFIGURE_TOTP` при первом входе.
- После настройки одноразового пароля стандартный browser flow Keycloak требует его ввод при каждом входе (проверено: попытка входа без настроенного OTP редиректит на `login-actions/required-action?execution=CONFIGURE_TOTP`).

## Яндекс ID

Яндекс OAuth - это обычный OAuth 2.0, а не OpenID Connect: он не понимает scope `openid` и отдаёт в userinfo свои собственные поля (`id`, `login`, `default_email` и т.д.), а не стандартные OIDC-claims. Встроенный в Keycloak тип identity-провайдера `oidc` безусловно добавляет `openid` в scope любого запроса авторизации (это зашито в конструкторе `OIDCIdentityProvider`, конфигом не отключается), поэтому Яндекс отвечал `invalid_scope` при попытке использовать этот тип напрямую.

Решение - собственное расширение Keycloak (Java SPI), а не костыль в конфиге:

- `keycloak/custom-providers/yandex-idp/` - исходники кастомного identity-провайдера `YandexIdentityProvider`/`YandexIdentityProviderFactory`, наследуются от `AbstractOAuth2IdentityProvider` напрямую (тот же базовый класс, что использует встроенный провайдер GitHub), минуя OIDC-специфичную логику. Сам определяет scope (`login:info login:email`, без `openid`), ходит за профилем на `login.yandex.ru/info` и парсит поля `id`/`login`/`default_email`/`first_name`/`last_name`.
- `keycloak/Dockerfile` - multi-stage сборка: компилирует провайдер против jar'ов самого Keycloak (без Maven/интернет-зависимостей, кроме базовых образов) и кладёт готовый `yandex-idp.jar` в `/opt/keycloak/providers/` финального образа. `docker-compose.yaml` теперь собирает `keycloak` из этого Dockerfile вместо использования образа "как есть".
- В `keycloak/realm-export.json` у Identity Provider `yandex` `providerId` указывает на наш провайдер (`yandex`), а не на встроенный `oidc`.
- Экран согласия реализован штатным механизмом Keycloak `first broker login` (Review Profile) - после аутентификации в Яндексе пользователь подтверждает использование своих профильных данных перед созданием учётной записи в BionicPRO.
- `clientId` указан в `realm-export.json` открытым текстом (не секрет - виден в адресной строке при переходе на страницу авторизации Яндекса). `clientSecret` в файле не хранится: используется Keycloak Vault (`${vault.yandex_client_secret}`, провайдер `file`, директория `keycloak/vault/` подключена как `--vault-dir`). Сам секрет - в незакоммиченном файле `keycloak/vault/reports-realm_yandex__client__secret` (двойное подчёркивание - экранирование ключа при формировании имени файла, см. `keycloak/vault/README.md`).

Проверено полным живым циклом: кнопка "Yandex ID" на форме логина Keycloak -> редирект на `oauth.yandex.ru/authorize` со scope `login:info login:email` (без `openid`, без ошибки `invalid_scope`) -> подтверждение доступа реальным пользователем на стороне Яндекса -> колбэк на `/realms/reports-realm/broker/yandex/endpoint`, обмен кода на токен с `clientSecret` из vault -> экран Review Profile -> успешный вход в bionicpro-auth с рабочей сессией и доступом к `/reports`.

## Проверка

Полный стек (`keycloak_db`, `ldap`, `keycloak`, `bionicpro-auth`, `frontend`) поднят через `docker-compose up` и проверен:

- Realm импортируется в Keycloak без ошибок, все клиенты, роли, LDAP-провайдер и Yandex IdP создаются автоматически.
- LDAP full sync успешно импортирует пользователей с ролями (`0 failed`).
- Полный цикл PKCE + BFF: `GET /auth/login` -> форма логина Keycloak -> POST credentials -> редирект на фронтенд с session cookie -> `GET /reports` с cookie возвращает 200 и подтверждает ротацию session id на каждом запросе -> `POST /auth/logout` инвалидирует сессию (последующий `GET /auth/session` возвращает 401).
- `accessTokenLifespan` в realm равен 120 секундам, `revokeRefreshToken` включён.
- Для пользователя без настроенного OTP при логине запрашивается обязательная настройка TOTP.
- Кастомный identity-провайдер `yandex-idp` собирается без ошибок, Keycloak при старте регистрирует его (`KC-SERVICES0047: yandex ... YandexIdentityProviderFactory`), и полный вход через Yandex ID пройден вручную от кнопки на форме логина до возврата в приложение с активной сессией.

Итоговый realm после всех манипуляций экспортирован в [`keycloak/keycloak-results-export.json`](../keycloak/keycloak-results-export.json).
