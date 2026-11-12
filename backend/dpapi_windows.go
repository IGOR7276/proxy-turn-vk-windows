//go:build windows
// +build windows

package backend

import (
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// DPAPI (Q10): пароли профилей и секреты деплоя шифруются на диске ключом
// текущего пользователя Windows. Это защищает от простого чтения JSON-файлов
// из %APPDATA% другим пользователем/после копирования на другую машину:
// CryptUnprotectData вернёт ошибку, и приложение попросит ввести пароль заново.
//
// Формат хранения: "dpapi:v1:" + base64(blob). Строки без префикса считаются
// legacy-plaintext и читаются как есть (миграция без потери данных).
const dpapiPrefix = "dpapi:v1:"

// CRYPTPROTECT_UI_FORBIDDEN — не показывать системный диалог подтверждения.
const cryptProtectUIForbidden = 0x1

var (
	crypt32DLL             = syscall.NewLazyDLL("crypt32.dll")
	kernel32DLL            = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData   = crypt32DLL.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32DLL.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32DLL.NewProc("LocalFree")
)

// dataBlob — DATA_BLOB из dpapi.h.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newDataBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

func (b *dataBlob) bytes() []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	return unsafe.Slice(b.pbData, b.cbData)
}

func (b *dataBlob) free() {
	if b.pbData != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(b.pbData)))
		b.pbData = nil
		b.cbData = 0
	}
}

// dpapiProtectString шифрует строку. Пустая строка и уже зашифрованная
// возвращаются без изменений (идемпотентность при повторном сохранении).
func dpapiProtectString(plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, dpapiPrefix) {
		return plain, nil
	}
	in := newDataBlob([]byte(plain))
	var out dataBlob
	ok, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, // pDataDescription
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(in)
	if ok == 0 {
		return "", fmt.Errorf("CryptProtectData: %w", callErr)
	}
	defer out.free()
	return dpapiPrefix + base64.StdEncoding.EncodeToString(out.bytes()), nil
}

// dpapiUnprotectString расшифровывает строку. Legacy-plaintext возвращается
// как есть, чтобы старые конфиги продолжали работать после обновления.
func dpapiUnprotectString(stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, dpapiPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, dpapiPrefix))
	if err != nil {
		return "", fmt.Errorf("dpapi base64: %w", err)
	}
	in := newDataBlob(raw)
	var out dataBlob
	ok, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(in)
	if ok == 0 {
		return "", fmt.Errorf("CryptUnprotectData: %w", callErr)
	}
	defer out.free()
	return string(out.bytes()), nil
}
