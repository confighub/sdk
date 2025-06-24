// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"os"
	"syscall"

	"github.com/confighub/sdk/function/internal/handlers/kubernetes"
	"github.com/confighub/sdk/function/internal/handlers/opentofu"
	"github.com/confighub/sdk/function/internal/handlers/properties"
	"github.com/confighub/sdk/function/handler"

	"github.com/labstack/echo/v4"
)

var kubernetesHandler *handler.FunctionHandler
var propertiesHandler *handler.FunctionHandler
var opentofuHandler *handler.FunctionHandler

func registerFunctionHandler(parent *echo.Group, h **handler.FunctionHandler, p handler.FunctionProvider) {
	*h = handler.NewFunctionHandler()
	p.RegisterFunctions(*h)
	p.SetPathRegistry(*h)
	group := parent.Group(p.GetToolchainPath())
	setupToolchainRootAPI(group, *h)

}

func echoSetup(rootRouter *echo.Echo) {
	apiRouter := rootRouter.Group("/function")
	setupAPIRootAPI(apiRouter)

	registerFunctionHandler(apiRouter, &kubernetesHandler, kubernetes.KubernetesRegistrar)
	registerFunctionHandler(apiRouter, &propertiesHandler, properties.PropertiesRegistrar)
	registerFunctionHandler(apiRouter, &opentofuHandler, opentofu.OpenTofuRegistrar)
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
