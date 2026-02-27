package radar

import (
	"fmt"
	"net/http"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/meta"
)

type webActor struct {
	act.Actor
}

func factoryWeb() gen.ProcessBehavior {
	return &webActor{}
}

func (w *webActor) Init(args ...any) error {
	if len(args) < 3 {
		return fmt.Errorf("radar web: expected 3 args (mux, host, port), got %d", len(args))
	}

	mux, ok := args[0].(*http.ServeMux)
	if ok == false {
		return fmt.Errorf("radar web: args[0] is not *http.ServeMux")
	}
	host, ok := args[1].(string)
	if ok == false {
		return fmt.Errorf("radar web: args[1] is not string")
	}
	port, ok := args[2].(uint16)
	if ok == false {
		return fmt.Errorf("radar web: args[2] is not uint16")
	}

	serverOptions := meta.WebServerOptions{
		Port:    port,
		Host:    host,
		Handler: mux,
	}
	webserver, err := meta.CreateWebServer(serverOptions)
	if err != nil {
		return err
	}
	if _, err := w.SpawnMeta(webserver, gen.MetaOptions{}); err != nil {
		return err
	}
	return nil
}
