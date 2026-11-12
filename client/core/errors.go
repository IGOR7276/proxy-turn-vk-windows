package core

import "errors"

// ErrTurnQuota — TURN Allocate отклонён VK по квоте (error 486).
//
// Квота считается на VK-аккаунт/IP, а не на конкретные TURN-креды, поэтому
// обновлять креды при ней бессмысленно: нужно подождать 30–60 секунд.
var ErrTurnQuota = errors.New("TURN квота исчерпана (486)")
