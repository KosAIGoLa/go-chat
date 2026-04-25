package main

import "github.com/ck-chat/ck-chat/internal/app"

func main() {
	app.Main(app.Service{Name: "fanout"})
}
