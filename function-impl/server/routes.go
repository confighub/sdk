// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"os"
	"syscall"

	"github.com/confighub/sdk/function/handler"
	"github.com/confighub/sdk/function-impl/appyaml"
	"github.com/confighub/sdk/function-impl/confighub"
	"github.com/confighub/sdk/function-impl/ini"
	"github.com/confighub/sdk/function-impl/kubernetes"
	"github.com/confighub/sdk/function-impl/opentofu"
	"github.com/confighub/sdk/function-impl/properties"
	"github.com/confighub/sdk/function-impl/toml"

	"github.com/labstack/echo/v4"
)

type FunctionServer struct {
	confighubHandler  *handler.FunctionHandler
	kubernetesHandler *handler.FunctionHandler
	propertiesHandler *handler.FunctionHandler
	opentofuHandler   *handler.FunctionHandler
	appyamlHandler    *handler.FunctionHandler
	tomlHandler       *handler.FunctionHandler
	iniHandler        *handler.FunctionHandler
}

func registerFunctionHandler(parent *echo.Group, h **handler.FunctionHandler, p handler.FunctionProvider) {
	*h = handler.NewFunctionHandler()
	p.RegisterFunctions(*h)
	p.SetPathRegistry(*h)
	group := parent.Group(p.GetToolchainPath())
	setupToolchainRootAPI(group, *h)

}

func echoSetup(rootRouter *echo.Echo) {
	s := &FunctionServer{}
	apiRouter := rootRouter.Group("/function")
	setupAPIRootAPI(apiRouter)

	registerFunctionHandler(apiRouter, &s.confighubHandler, confighub.NewConfigHubRegistrar())
	registerFunctionHandler(apiRouter, &s.kubernetesHandler, kubernetes.NewKubernetesRegistrar())
	registerFunctionHandler(apiRouter, &s.propertiesHandler, properties.NewPropertiesRegistrar())
	registerFunctionHandler(apiRouter, &s.appyamlHandler, appyaml.NewAppConfigYAMLRegistrar())
	registerFunctionHandler(apiRouter, &s.tomlHandler, toml.NewTOMLRegistrar())
	registerFunctionHandler(apiRouter, &s.iniHandler, ini.NewINIRegistrar())
	registerFunctionHandler(apiRouter, &s.opentofuHandler, opentofu.NewOpenTofuRegistrar())
}

func setupAPIRootAPI(apiRouter *echo.Group) {
	apiRouter.GET("/ok", basicOk())
	apiRouter.GET("/info", infoHandler())
	apiRouter.POST("/shutdown", shutdownHandler())
}

func infoHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		// TODO: Decide what info to return.
		return c.JSON(http.StatusOK, "OK")
	}
}

func shutdownHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		process, _ := os.FindProcess(os.Getpid())
		process.Signal(syscall.SIGINT)
		return c.JSON(http.StatusOK, "OK")
	}
}

func setupToolchainRootAPI(toolchainRoot *echo.Group, fh *handler.FunctionHandler) {
	toolchainRoot.POST("", fh.Invoke)
	toolchainRoot.GET("", fh.List)
	toolchainRoot.GET("/paths", fh.ListPaths)
}

func basicOk() echo.HandlerFunc {
	return func(c echo.Context) error {
		// Sanity check for UI routing
		return c.JSON(http.StatusOK, "OK")
	}
}
