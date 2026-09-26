package chi

import (
	"net/http"

	chiv5 "github.com/go-chi/chi/v5"
	"github.com/universe-10th/rest-resources/internal/nethttpadapter"
	"github.com/universe-10th/rest-resources/types/services"
)

var (
	ErrInvalidRouter      = nethttpadapter.ErrInvalidRouter
	ErrInvalidService     = nethttpadapter.ErrInvalidService
	ErrInvalidRootService = nethttpadapter.ErrInvalidRootService
)

type Context = nethttpadapter.Context

func WrapContext(response http.ResponseWriter, request *http.Request) *Context {
	return nethttpadapter.WrapContext(response, request)
}

func MustInstall(router chiv5.Router, service services.Service) {
	nethttpadapter.MustInstall(router, service)
}

func Install(router chiv5.Router, service services.Service) error {
	return nethttpadapter.Install(router, service)
}
