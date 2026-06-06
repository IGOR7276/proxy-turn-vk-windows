# WDTT

Нативный Windows-клиент туннеля через VK TURN на базе WireGuard.

Форк https://github.com/amurcanov/proxy-turn-vk-android  с полностью переписанным десктопным UI на Wails

## Возможности

### UI
- **Туннель** — выбор сервера, хеши VK, вставка `wdtt://` ссылки, кнопка «Подключить» / «Отключить»
- **Деплой** — установка VPS-сервера по SSH: загрузка `wdtt-server`, настройка systemd, Telegram-уведомления
- **Логи** — live-tail всех событий туннеля
- **Настройки** — DNS-прокси, AutoWG, MTU, поведение, тема
- **Инфо** — версия, статистика сессии, ссылки

## Требования

- Windows 10/11 x64
- **Запуск от администратора** (нужен доступ к WG-драйверу)
- VPS с Linux и открытыми портами (по умолчанию 22 SSH, 56000 DTLS, 56001 WG)
- VK-аккаунт с хешем звонка

## Запуск

Скачать из релиза, распоквать, запустить wdtt от имени администратора

## Сборка из исходников

```powershell
# Зависимости
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Клонируем
git clone https://github.com/IGOR7276/proxy-turn-vk-windows.git
cd proxy-turn-vk-windows

# Сборка 
wails build
```

Результат: `build/bin/wdtt.exe` 

**Быстрая dev-сборка** (без  иконки):
```powershell
wails build -nopackage
```


## Получить VK-хеш

VK → группа → звонок → «Пригласить» → ссылка вида `https://vk.com/call/join?hash=XXXXXXXXX` → код после `hash=`.


## Лицензия

GNU GPL v3