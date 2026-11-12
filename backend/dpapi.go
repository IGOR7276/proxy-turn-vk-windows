package backend

import "log"

// protectSecret — безопасная обёртка над DPAPI для путей сохранения.
// При ошибке шифрования возвращает исходную строку: потерять пароль хуже,
// чем сохранить его в открытом виде (и в логе будет предупреждение).
func protectSecret(s string) string {
	if s == "" {
		return s
	}
	out, err := dpapiProtectString(s)
	if err != nil {
		log.Printf("[DPAPI] Не удалось зашифровать секрет: %v", err)
		return s
	}
	return out
}

// unprotectSecret — обратная операция. Legacy-plaintext и битые blob'ы
// возвращаются как есть, чтобы UI мог показать поле и пользователь ввёл
// пароль заново.
func unprotectSecret(s string) string {
	if s == "" {
		return s
	}
	out, err := dpapiUnprotectString(s)
	if err != nil {
		log.Printf("[DPAPI] Не удалось расшифровать секрет: %v", err)
		return s
	}
	return out
}

// protectProfile/unprotectProfile — единая точка шифрования пароля профиля.
// Пароль шифруется только на диске: в памяти и в UI он всегда открытый.
func protectProfile(p ProfileData) ProfileData {
	p.Password = protectSecret(p.Password)
	return p
}

func unprotectProfile(p *ProfileData) {
	if p != nil {
		p.Password = unprotectSecret(p.Password)
	}
}
