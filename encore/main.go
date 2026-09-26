package encore

import (
	"net/http"

	chiv5 "github.com/go-chi/chi/v5"
	"github.com/universe-10th/rest-resources/internal/nethttpadapter"
	"github.com/universe-10th/rest-resources/types/services"
)

var (
	ErrInvalidService     = nethttpadapter.ErrInvalidService
	ErrInvalidRootService = nethttpadapter.ErrInvalidRootService
)

type Context = nethttpadapter.Context

func WrapContext(response http.ResponseWriter, request *http.Request) *Context {
	return nethttpadapter.WrapContext(response, request)
}

func MustNewHandler(services_ ...services.Service) http.Handler {
	router := chiv5.NewRouter()
	for _, service := range services_ {
		nethttpadapter.MustInstall(router, service)
	}
	return router
}

func NewHandler(services_ ...services.Service) (handler http.Handler, err error) {
	defer func() {
		if v := recover(); v != nil {
			if err_, ok := v.(error); ok {
				handler = nil
				err = err_
			} else {
				panic(v)
			}
		}
	}()

	return MustNewHandler(services_...), nil
}
