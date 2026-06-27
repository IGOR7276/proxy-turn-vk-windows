package backend

import (
	"log"
	"strings"

	"wg-turn-client/core"
)

// ─── Wails API для управления исключениями по доменам ───────────────────────
//
// Хранилище: %APPDATA%/wdtt/exclude_domains.json
// Формат: ["example.com", "*.bank.ru", "localhost"]
//
// Wildcard-паттерны: *.example.com матчит все поддомены включая сам домен.
// Точные паттерны: example.com матчит только example.com.
//
// Применяются при следующем запуске туннеля (не во время активной сессии —
// для применения нужно переподключиться).

// GetExcludeDomains возвращает текущий список исключённых доменов.
func (a *App) GetExcludeDomains() []string {
	domains, err := core.LoadExcludeDomains(configDir())
	if err != nil {
		log.Printf("[EXCL] Ошибка загрузки exclude_domains.json: %v", err)
		return []string{}
	}
	if domains == nil {
		return []string{}
	}
	return domains
}

// AddExcludeDomain добавляет домен в список исключений.
// Валидирует формат (не пустой, не содержит пробелов и протокола).
// Дубликаты игнорируются.
func (a *App) AddExcludeDomain(domain string) error {
	domain = normalizeDomain(domain)
	if err := validateDomain(domain); err != nil {
		return err
	}

	domains, err := core.LoadExcludeDomains(configDir())
	if err != nil {
		return err
	}
	for _, d := range domains {
		if d == domain {
			return nil // уже есть — идемпотентно
		}
	}
	domains = append(domains, domain)
	if err := core.SaveExcludeDomains(configDir(), domains); err != nil {
		return err
	}
	log.Printf("[EXCL] Добавлен домен: %s (всего: %d)", domain, len(domains))
	return nil
}

// RemoveExcludeDomain удаляет домен из списка исключений.
// Если домена нет — возвращает nil (идемпотентно).
func (a *App) RemoveExcludeDomain(domain string) error {
	domain = normalizeDomain(domain)

	domains, err := core.LoadExcludeDomains(configDir())
	if err != nil {
		return err
	}
	out := domains[:0]
	found := false
	for _, d := range domains {
		if d == domain {
			found = true
			continue
		}
		out = append(out, d)
	}
	if !found {
		return nil
	}
	if err := core.SaveExcludeDomains(configDir(), out); err != nil {
		return err
	}
	log.Printf("[EXCL] Удалён домен: %s (осталось: %d)", domain, len(out))
	return nil
}

// SaveExcludeDomains заменяет весь список исключений.
// Используется для bulk-операций (импорт/экспорт, сброс).
func (a *App) SaveExcludeDomains(domains []string) error {
	clean := make([]string, 0, len(domains))
	for _, d := range domains {
		d = normalizeDomain(d)
		if d == "" {
			continue
		}
		if err := validateDomain(d); err != nil {
			return err
		}
		clean = append(clean, d)
	}
	if err := core.SaveExcludeDomains(configDir(), clean); err != nil {
		return err
	}
	log.Printf("[EXCL] Список исключений заменён (%d доменов)", len(clean))
	return nil
}

// normalizeDomain приводит домен к каноническому виду (lowercase, trim).
func normalizeDomain(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}

// validateDomain проверяет корректность паттерна домена.
// Допускает wildcards (только префиксный "*.example.com").
func validateDomain(d string) error {
	if d == "" {
		return errEmptyDomain
	}
	if strings.ContainsAny(d, " \t\n\r") {
		return errDomainHasSpaces
	}
	if strings.Contains(d, "://") {
		return errDomainHasScheme
	}
	if strings.Contains(d, "/") {
		return errDomainHasPath
	}
	// Wildcard допустим только в начале и только один раз.
	if strings.Count(d, "*") > 1 {
		return errDomainMultiWildcard
	}
	if strings.Contains(d, "*") && !strings.HasPrefix(d, "*.") {
		return errDomainBadWildcard
	}
	// Базовая проверка: должен содержать точку (кроме "localhost").
	if d != "localhost" && !strings.Contains(d, ".") {
		return errDomainNoDot
	}
	return nil
}

type domainError struct{ msg string }

func (e *domainError) Error() string { return e.msg }

var (
	errEmptyDomain       = &domainError{"домен не может быть пустым"}
	errDomainHasSpaces   = &domainError{"домен не должен содержать пробелов"}
	errDomainHasScheme   = &domainError{"домен не должен содержать схему (http://, https://)"}
	errDomainHasPath     = &domainError{"домен не должен содержать путь (/...)"}
	errDomainMultiWildcard = &domainError{"допускается только один wildcard (*) в начале паттерна"}
	errDomainBadWildcard = &domainError{"wildcard (*) допускается только в формате *.example.com"}
	errDomainNoDot       = &domainError{"домен должен содержать точку (например, example.com)"}
)
