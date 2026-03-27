# vk-turn-proxy

Прокси-транспорт для передачи трафика через TURN (VK/Telemost) с DTLS-обёрткой, multi-stream режимом, авто-discovery TURN и генерацией конфигов для V2Ray/Xray/sing-box.

## Содержание
- [Быстрый старт](#быстрый-старт)
- [TSPU Full Bypass Mode](#tspu-full-bypass-mode)
- [Флаги клиента](#флаги-клиента)
- [Флаги сервера](#флаги-сервера)
- [Платформенные инструкции](#платформенные-инструкции)
- [V2Ray/Xray/sing-box](#v2rayxraysing-box)
- [Advanced Normalization Options](#advanced-normalization-options)
- [Troubleshooting](#troubleshooting)

## Быстрый старт

### 1) Сервер
```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:<wireguard_port>
```

### 2) Клиент
```bash
./client -peer <server_ip:56000> -vk-link <https://vk.com/call/join/...> -listen 127.0.0.1:9000
```

### 3) WireGuard
- Endpoint в клиентском WG-конфиге: `127.0.0.1:9000`
- MTU: `1280`

---

## Короткая инструкция для обычных пользователей

Ниже — максимально простой сценарий без «тонкой» настройки.

### Что нужно заранее
1. **Ваш VPN-сервер с WireGuard** (установленный и рабочий).
2. **Ссылка на звонок**:
   - ВКонтакте: `https://vk.com/call/join/...` (флаг `-vk-link`),
   - или Яндекс Телемост (флаг `-yandex-link`).
3. **Файлы программы** `client` и `server` для вашей платформы.

### Шаг 1. Запустите сервер (один раз на сервере)
```bash
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:<порт_WireGuard>
```

### Шаг 2. Настройте WireGuard на вашем устройстве
- В клиентском конфиге WireGuard замените `Endpoint` на `127.0.0.1:9000`.
- Установите `MTU = 1280`.

### Шаг 3. Запустите клиент
Минимальный вариант:
```bash
./client -peer <IP_сервера:56000> -vk-link <ссылка_на_звонок> -listen 127.0.0.1:9000
```

Если у вас Телемост, используйте `-yandex-link` вместо `-vk-link`.

### Шаг 4. Включите WireGuard
Включайте туннель **после** запуска `client`.

### Быстрые решения проблем
- **Медленно или нестабильно**: добавьте `-n 1`.
- **Не соединяется через обычный режим**: попробуйте `-udp`.
- **На Linux/Windows есть конфликт маршрутов**: запускайте через `routes.sh` / `routes.ps1` (см. раздел «Платформенные инструкции» ниже).

## TSPU Full Bypass Mode

Режим из одного запуска (как в исходной документации):

```bash
./client -auto-turn -mimic-vk -n 4 -listen 127.0.0.1:9000 -peer <server_ip:56000> -vk-link <link>
```

Что включает запуск:
- discovery TURN с fallback на hardcoded список;
- packet shaping/jitter/padding;
- параллельные потоки (по умолчанию 4);
- встроенный pre-flight probe (если включён `-probe`).

---

## Флаги клиента

| Флаг | По умолчанию | Описание |
|---|---:|---|
| `-peer` | — | Адрес удалённого сервера (`host:port`), обязателен. |
| `-listen` | `127.0.0.1:9000` | Локальный UDP listener для туннеля. |
| `-vk-link` / `-yandex-link` | — | Ссылка для получения TURN credentials/endpoint. Один из двух обязателен. |
| `-turn` | `""` | Ручной override TURN IP/host. |
| `-port` | `""` | Ручной override порта TURN. |
| `-auto-turn` | `false` | Включает discovery TURN. |
| `-mimic-vk` | `false` | Включает профиль под размеры/тайминги голосового трафика. |
| `-padding-max` | `512` | Максимум добавочного padding (байты). |
| `-jitter` | `50` | Верхняя граница jitter (мс). |
| `-n` | `4` | Количество потоков (ограничено `1..32`). |
| `-udp` | `false` | UDP-only режим подключения к TURN. |
| `-rotate-turn` | `false` | Ротация TURN по таймеру/счётчику пакетов. |
| `-no-dtls` | `false` | Отключает DTLS-обёртку (не рекомендуется). |
| `-hysteria` | `""` | Путь к JSON-конфигу inner tunnel Hysteria2. |
| `-probe` | `true` | Pre-flight probe сети перед установкой канала. |
| `-gen-v2ray-client` | `""` | Сгенерировать шаблон client JSON. |
| `-gen-v2ray-server` | `""` | Сгенерировать шаблон server JSON. |

## Флаги сервера

| Флаг | По умолчанию | Описание |
|---|---:|---|
| `-listen` | `0.0.0.0:56000` | Внешний адрес приёма трафика от client. |
| `-connect` | `127.0.0.1:51820` | Адрес назначения (обычно WireGuard). |
| `-udp` | `false` | Обработка UDP трафика без TCP fallback. |

---

## Платформенные инструкции

### Android / Termux
1. В WG-клиенте выставьте endpoint `127.0.0.1:9000`, `MTU=1280`.
2. Добавьте только необходимые приложения в exclusions.
3. Для долгой сессии в Termux:
   ```bash
   termux-wake-lock
   ```
4. Запуск:
   ```bash
   ./client-android -peer <server:56000> -vk-link <link> -listen 127.0.0.1:9000
   ```

### Linux
```bash
./client -peer <server:56000> -vk-link <link> -listen 127.0.0.1:9000 | sudo ./routes.sh
```

### Windows (PowerShell от администратора)
```powershell
./client.exe -peer <server:56000> -vk-link <link> -listen 127.0.0.1:9000 | ./routes.ps1
```

---

## V2Ray/Xray/sing-box

Готовые шаблоны:
- `configs/v2ray-client.json`
- `configs/v2ray-server.json`

Автогенерация:
```bash
./client -peer <server:56000> -vk-link <link> \
  -gen-v2ray-client configs/v2ray-client.json \
  -gen-v2ray-server configs/v2ray-server.json
```

---

## Advanced Normalization Options

Расширенные параметры нормализации/шумоподобия задаются текущими флагами:
- `-mimic-vk`
- `-padding-max`
- `-jitter`
- `-n`
- `-rotate-turn`

Рекомендуемые профили:

| Профиль | Команда |
|---|---|
| Conservative | `./client -peer <peer> -vk-link <link> -n 1 -padding-max 64 -jitter 20` |
| Balanced | `./client -peer <peer> -vk-link <link> -n 4 -mimic-vk -padding-max 256 -jitter 35` |
| Aggressive | `./client -peer <peer> -vk-link <link> -n 8 -mimic-vk -padding-max 512 -jitter 50 -rotate-turn` |

---

## Troubleshooting

### Не подключается вообще
- Проверьте, что задан `-peer` и один из `-vk-link` / `-yandex-link`.
- Для диагностики выключите сложные режимы: `-n 1` и без `-rotate-turn`.

### Нестабильный канал / частые реконнекты
- Уменьшите `-n` до `1` или `2`.
- Уменьшите `-padding-max` и `-jitter`.
- Проверьте MTU: должен быть `1280`.

### Не работает TCP путь до TURN
- Попробуйте `-udp`.
- Явно задайте TURN: `-turn <ip> -port 3478`.

### DNS/маршрутизация конфликтует с VPN
- На Android включите split tunneling только для нужных приложений.
- На Linux/Windows используйте `routes.sh`/`routes.ps1` и не поднимайте VPN до готовности клиента.

---

