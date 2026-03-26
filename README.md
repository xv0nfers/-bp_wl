# Good TURN
Проброс трафика WireGuard/Hysteria через TURN сервера VK звонков или Яндекс телемоста. Пакеты шифруются DTLS 1.2, затем параллельными потоками через TCP или UDP отправляются на TURN сервер по протоколу STUN ChannelData. Оттуда по UDP отправляются на ваш сервер, где расшифровываются и передаются в WireGuard. Логин/пароль от TURN генерируются из ссылки на звонок.

Только для учебных целей!

## TSPU Full Bypass Mode

Одна команда для максимальной обфускации:

```bash
./client -auto-turn -mimic-vk -n 4 -listen 127.0.0.1:9000
```

Что включается:
- Dynamic TURN discovery (`-auto-turn`) + fallback на `5.255.211.24x`.
- DPI evasion: DTLS 1.2, random padding, jitter, mimic VK packet profile.
- TSPU probe: до старта проверяет признаки блокировок и автоматически включает max evasion.
- Failover/rotation TURN: `-rotate-turn` (по 5 минут или ~10k пакетов).
- Multi-stream: `-n 1..32` (по умолчанию `4`).
- UDP-only mode: `-udp` + TCP fallback.
- Hysteria2 inner tunnel: `-hysteria /path/to/config.json`.
- V2Ray/Xray/sing-box bridge: см. `configs/v2ray-client.json` и `configs/v2ray-server.json`.

## Настройка
Нам понадобится:
1. Ссылка на действующий ВК звонок: создаём свой (нужен аккаунт вк), или гуглим `"https://vk.com/call/join/"`.
Ссылка действительна вечно, если не нажимать "завершить звонок для всех"
2. Или ссыска на звонок Яндекс телемоста: `"https://telemost.yandex.ru/j/"`. Её лучше не гуглить, так как видно подключение к конференции
3. VPS с установленным WireGuard
4. Для андроида: скачать Termux из F-Droid
### Сервер
```
./server -listen 0.0.0.0:56000 -connect 127.0.0.1:<порт wg>
```
### Клиент
#### Android

**Рекомендуемый способ:**
Использовать нативное Android-приложение [vk-turn-proxy-android](https://github.com/MYSOREZ/vk-turn-proxy-android).
- В клиентском конфиге WireGuard меняем адрес сервера на `127.0.0.1:9000`, ставим MTU 1280
-  **Добавляем приложение в исключения WireGuard. Нажимаем "сохранить".**

**Альтернативный способ (через Termux):**
- В клиентском конфиге WireGuard меняем адрес сервера на `127.0.0.1:9000`, ставим MTU 1280
-  **Добавляем Termux в исключения WireGuard. Нажимаем "сохранить".**
В Termux:
```
termux-wake-lock
```
Телефон не будет уходить в глубокий сон, так что на ночь ставьте на зарядку. Чтобы отключить:
```
termux-wake-unlock
```
Копируем бинарник в локальную папку, даём права на исполнение:
```
cp /sdcard/Download/client-android ./
chmod 777 ./client-android
```
Запускаем:
```
./client-android -peer <ip сервера wg>:56000 -vk-link <VK ссылка> -listen 127.0.0.1:9000
```
Или
```
./client-android -udp -turn 5.255.211.241 -peer <ip сервера wg>:56000 -yandex-link <Ya ссылка> -listen 127.0.0.1:9000
```

**Если после включения VPN в терминале вылезают ошибки DNS, попробуйте в Wireguard включить VPN только для нужных приложений.**
#### Linux
В клиентском конфиге WireGuard меняем адрес сервера на `127.0.0.1:9000`, ставим MTU 1280

Скрипт будет добавлять маршруты к нужным ip:

```
./client-linux -peer <ip сервера wg>:56000 -vk-link <VK ссылка> -listen 127.0.0.1:9000 | sudo routes.sh
```

```
./client-linux -udp -turn 5.255.211.241 -peer <ip сервера wg>:56000 -yandex-link <Ya ссылка> -listen 127.0.0.1:9000 | sudo routes.sh
```

Не включайте впн, пока программа не установит соединение! В отличие от андроида, здесь часть запросов будет идти через впн (dns и запрос подключения к turn)
#### Windows
В клиентском конфиге WireGuard меняем адрес сервера на `127.0.0.1:9000`, ставим MTU 1280

В PowerShell от Администратора (чтобы скрипт прописывал маршруты):

```
./client.exe -peer <ip сервера wg>:56000 -vk-link <VK ссылка> -listen 127.0.0.1:9000 | routes.ps1
```

```
./client.exe -udp -turn 5.255.211.241 -peer <ip сервера wg>:56000 -yandex-link <Ya ссылка> -listen 127.0.0.1:9000 | routes.ps1
```

Не включайте впн, пока программа не установит соединение! В отличие от андроида, здесь часть запросов будет идти через впн (dns и запрос подключения к turn)
### Если не работает
С помощью опции `-turn` можно указать адрес TURN сервера вручную. Это должен быть сервер ВК, Макса или Одноклассников (ссылка вк) или Яндекса (ссылка яндекса). Возможно потом составлю список.

Если не работает TCP, попробуйте добавить флаг `-udp`.

Добавьте флаг `-n 1` для более стабильного подключения в 1 поток (ограничение 5 Мбит/с для ВК)

## v2ray

Вместо WireGuard можно использовать любое V2Ray-ядро которое его поддерживает (например, xray или sing-box) и любой V2Ray-клиент который использует это ядро (например, v2rayN или v2rayNG). С помощью их вы сможете добавить больше входящих интерфейсов (например, SOCKS) и реализовать точечный роутинг.

Готовые конфиги:
- `configs/v2ray-client.json`
- `configs/v2ray-server.json`

Авто-генерация:
```bash
./client -gen-v2ray-client configs/v2ray-client.json -gen-v2ray-server configs/v2ray-server.json -vk-link <link> -peer <server:56000>
```


Пример конфигов:

<details>

<summary>
Клиент
</summary>

```json
{
    "inbounds": [
        {
            "protocol": "socks",
            "listen": "127.0.0.1",
            "port": 1080,
            "settings": {
                "udp": true
            },
            "sniffing": {
                "enabled": true,
                "destOverride": [
                    "http",
                    "tls"
                ]
            }
        },
        {
            "protocol": "http",
            "listen": "127.0.0.1",
            "port": 8080,
            "sniffing": {
                "enabled": true,
                "destOverride": [
                    "http",
                    "tls"
                ]
            }
        }
    ],
    "outbounds": [
        {
            "protocol": "wireguard",
            "settings": {
                "secretKey": "<client secret key>",
                "peers": [
                    {
                        "endpoint": "127.0.0.1:9000",
                        "publicKey": "<server public key>"
                    }
                ],
                "domainStrategy": "ForceIPv4",
                "mtu": 1280
            }
        }
    ]
}
```

</details>

<details>

<summary>
Сервер
</summary>

```json
{
    "inbounds": [
        {
            "protocol": "wireguard",
            "listen": "0.0.0.0",
            "port": 51820,
            "settings": {
                "secretKey": "<server secret key>",
                "peers": [
                    {
                        "publicKey": "<client public key>"
                    }
                ],
                "mtu": 1280
            },
            "sniffing": {
                "enabled": true,
                "destOverride": [
                    "http",
                    "tls"
                ]
            }
        }
    ],
    "outbounds": [
        {
            "protocol": "freedom",
            "settings": {
                "domainStrategy": "UseIPv4"
            }
        }
    ]
}
```

</details>

## Direct mode
С флагом `-no-dtls` можно отправлять пакеты без обфускации DTLS и подключаться к обычным серверам Wireguard. Может привести к бану от вк/яндекса.
