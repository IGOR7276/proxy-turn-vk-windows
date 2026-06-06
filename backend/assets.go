package backend

// Встроенные ресурсы. Заполняются через Init() из main.go.
// При Wails-сборке файлы кладутся в wdtt-embed и байты передаются сюда.
//
// При ручной go build без Wails можно оставить пустыми — deploy-кнопка
// тогда не сработает, но клиентская часть работать будет.

var (
	deployScript []byte
	serverBinary []byte
)

func Init(deploy, server []byte) {
	deployScript = deploy
	serverBinary = server
}
