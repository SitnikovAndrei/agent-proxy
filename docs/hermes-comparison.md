# Сравнение с Hermes Agent

Дата: 2026-10-13. Исследована ветка `main`, commit
`3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065`.
Репозиторий скачан shallow clone во временный каталог Windows; код не запускался.
Сравнение выполнено чтением исходников Hermes и текущего API_PROXY.
Рабочий код прокси и бинарники в рамках этой сверки не изменялись.

Последующая доработка 2026-10-13: реализованы извлечение account ID и региона
из JWT серверных credentials и сохранение дополнительных полей `auth.json`
при refresh. Затем реализованы refresh по JWT `exp`, межпроцессные блокировки
OCM/CCM и защита от повторного refresh после незавершённой ротации.
Ниже приведено исходное сравнение до этих доработок.

## Codex

### 1. Обновление access token — приоритет высокий

Hermes читает `exp` из JWT и обновляет токен за 120 секунд до истечения.
Под блокировкой повторно читает credentials и снова проверяет необходимость
refresh: другой процесс уже мог обновить файл.

У нас `oauthCredentials.needsRefresh()` учитывает только `last_refresh` и 8 дней.
Если access token истёк раньше, запрос уйдёт с истёкшим токеном. Требуется
учитывать срок действия при наличии корректного JWT, сохранив явно определённое
поведение для credentials без `exp`. JWT-декодирование здесь извлекает метаданные
серверных credentials; оно не заменяет проверку клиентской авторизации.

Источники: [auth_codex.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/hermes_cli/auth_codex.py#L65),
[auth_constants.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/hermes_cli/auth_constants.py#L93).

### 2. Владение refresh token и конкуренция — приоритет высокий

Основная Codex-сессия Hermes хранится в `~/.hermes/auth.json`. Реализован
отдельный вход, а не только использование общего файла Codex CLI. Импорт и
восстановление из CLI тоже существуют, поэтому изоляция не абсолютная.
Hermes использует межпроцессную блокировку транзакции чтение → refresh → запись.

У нас оба credential store используют канал внутри процесса. Повторное чтение
перед записью обнаруживает некоторые изменения файла, но не предотвращает
одновременное использование одного refresh token разными процессами. Собственный
lock прокси поможет между копиями прокси; Codex CLI не обязан соблюдать этот lock.
Для независимой работы надёжнее отдельная OAuth-сессия с собственным refresh token.
Простое копирование auth.json независимую сессию не создаёт.

Для Claude Hermes также учитывает незафиксированную ротацию: если refresh прошёл,
а сохранение новых credentials сорвалось, старый token помечается как потраченный.
У нас после ошибки записи следующий запрос может снова попытаться его использовать.

Источники: [auth_codex.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/hermes_cli/auth_codex.py#L1),
[auth.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/hermes_cli/auth.py#L720),
[anthropic_credentials.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/anthropic_credentials.py#L475).

### 3. Account ID и регион workspace — приоритет высокий для соответствующих аккаунтов

Hermes извлекает `chatgpt_account_id` и `chatgpt_data_residency` (либо
`chatgpt_compute_residency`) из JWT и устанавливает `ChatGPT-Account-ID` и
`x-openai-internal-codex-residency`.

У нас account ID берётся только из `tokens.account_id`, регион из credentials
не извлекается. Входящий заголовок региона пока просто передаётся. При общем
серверном аккаунте регион должен определяться его credentials. По комментариям
Hermes, отсутствие региона вызывает 401 у workspace с региональными ограничениями;
это утверждение авторов, запросом к такому аккаунту оно здесь не проверялось.

Источник: [codex_headers.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/codex_headers.py#L49).

### 4. Каталог моделей и версия клиента — приоритет средний

Hermes явно запрашивает `/models?client_version=99.0.0`, затем пробует `0.0.0`,
если первый ответ не 200 или не содержит непустой массив `models`.
По комментариям проекта, версия влияет на фильтр `minimal_client_version`,
а `0.0.0` на момент их наблюдения возвращала устаревший набор моделей.
Это обход совместимости Hermes, а не документированная гарантия OpenAI.

У нас подставляется реальная версия `0.162.1`; переданное клиентом значение
сохраняется. Это устраняет отсутствие обязательного параметра, но фиксированная
версия со временем может ограничить каталог. Нужна явная политика обновления
версии или отдельная настройка. Автоматически объявлять версию 99.0.0 не следует.
Исходное поле `models` у нас уже сохраняется полностью, поэтому метаданные Codex
не теряются. Добавленный `data` обеспечивает другой формат потребителя.

Hermes кэширует контексты моделей на час, отрицательный результат — на 5 минут;
ключ включает fingerprint токена и base URL. У нас каталог запрашивается каждый раз.
Если добавлять кэш, он должен учитывать серверный аккаунт и `client_version`.

Источник: [model_metadata.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/model_metadata.py#L1785).

### 5. Идентификация клиента и Responses

На официальном Codex endpoint Hermes задаёт свой `User-Agent` и
`originator: hermes-agent`. Из запроса также формируются session/cache headers.
У нас клиентские User-Agent, originator и session headers передаются; своих
значений по умолчанию нет. Клиент Codex обычно уже формирует их сам.

Hermes преобразует сообщения в Responses input, формирует instructions,
`store: false`, инструменты и replay reasoning. Есть отдельный транспорт
`codex app-server`. На Python 3.14+ реализовано zstd-сжатие исходящих запросов
Responses к официальному backend; это транспортная оптимизация.

У нас OAuth OCM передаёт тело Responses без изменения, chat completions
отклоняет. Поэтому произвольный OpenAI-клиент не получает автоматически
совместимость с ограничениями Codex backend. Конвертация Chat Completions,
SSE и tool calls была бы отдельным адаптером с собственным внешним контрактом.
Преобразования истории и запуск app-server относятся к ответственности harness.

Источники: [codex_headers.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/codex_headers.py),
[transports/codex.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/transports/codex.py#L730),
[codex_runtime.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/codex_runtime.py#L1).

## Claude

Hermes для OAuth добавляет оба beta-флага `claude-code-20250219` и
`oauth-2025-04-20`, User-Agent с версией Claude Code и `x-app: cli`.
CCM гарантирует только `oauth-2025-04-20`; остальные заголовки передаёт клиент
или задаёт конфигурация. Для самого Claude Code этого может быть достаточно,
для произвольного Anthropic-клиента такой гарантии нет.

Дополнительно Hermes меняет системный текст, имена tools и ссылки на них
в истории. Эти преобразования нельзя незаметно добавить в прозрачный прокси:
они меняют смысл и идентификаторы пользовательского запроса. Если они нужны,
требуется явный режим адаптера и обратное преобразование обычных и SSE-ответов.

У обоих проектов refresh Claude выполняется с запасом около минуты до истечения.
Hermes имеет межпроцессную координацию и работу с Keychain. Наш CCM уже сохраняет
неизвестные поля JSON при обновлении credentials; в OCM такой защиты нет:
перезапись через типизированный oauthCredentials теряет неизвестные поля auth.json.

Источники: [anthropic_adapter.py](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/anthropic_adapter.py#L234),
[OAuth headers](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/anthropic_adapter.py#L489),
[преобразование запроса](https://github.com/NousResearch/hermes-agent/blob/3b63f7b0a4c7beb9eadbce2f15b9da75d01f5065/agent/anthropic_adapter.py#L547).

## Рекомендуемый порядок доработки

1. Refresh по JWT expiry; корректное извлечение account ID и региона workspace.
2. Сохранение неизвестных полей OCM credentials при ротации.
3. Владение OAuth-сессией, межпроцессная координация и обработка ошибки фиксации ротации.
4. Явная политика версии каталога и идентификации клиентов обоих провайдеров.
5. При необходимости отдельный режим адаптации обычных API-клиентов.

Проверки были статическими. Тесты и реальные запросы с OAuth не выполнялись.
Поведение провайдеров, описанное в комментариях Hermes, отделено выше от
наблюдаемого кода и требует проверки на реальных аккаунтах.

### Самопроверка последующей доработки credentials

Новые функции короче 40 строк, Go-файлы короче 300 строк. Дополнительные поля
представлены `json.RawMessage` только на границе файлового JSON; JWT identity
разбирается в типизированную структуру с явно опциональными полями.
Повреждённые JWT возвращают ошибку; непрозрачные токены явно не имеют локальной
identity. Новых UNKNOWN/OTHER, широких обработчиков ошибок и повреждённой
кириллицы не найдено. Возврат `nil` для identity означает допустимое отсутствие,
возвраты `nil, err` соответствуют контракту ошибок Go.

При последующей доработке refresh запрос OAuth переведён на типизированную
структуру, сетевой запрос и разбор ответа разделены по ответственности.
Межпроцессная блокировка заменила каналы внутри каждого credential store;
сохранены повторное чтение, проверка смены credentials, таймаут, отмена и
обязательное сохранение после успешной ротации. Неизвестные поля CCM теперь
переносятся из актуального документа без повторного refresh из-за изменения
одних метаданных. Lock-файлы включены в проверку пересечения путей.

Прежнее представление отсутствующего account ID пустой строкой в `getAccountID()`
сохранено. Собственный OAuth login и координация с внешним Codex CLI/Claude Code
не реализованы; файловая блокировка действует между копиями этой прокси.
Сценарии отказа и реальный OAuth не проверялись исполнением тестов.
