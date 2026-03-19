// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package server

import (
	"net/http"
	"os"
	"syscall"

	"github.com/confighub/sdk/configkit/yamlkit"
	"github.com/confighub/sdk/function/handler"
	funcimpl "github.com/confighub/sdk/function-impl"

	"github.com/labstack/echo/v4"
)

func echoSetup(rootRouter *echo.Echo) {
	exec := funcimpl.NewStandardExecutor(nil)
	apiRouter := rootRouter.Group("/function")
	setupAPIRootAPI(apiRouter)

	for toolchain := range exec.RegisteredFunctions() {
		fh, ok := exec.GetHandler(toolchain)
		if !ok {
			continue
		}
		rp := fh.GetResourceProvider()
		path := yamlkit.GetToolchainPath(rp)
		group := apiRouter.Group(path)
		setupToolchainRootAPI(group, fh)
	}
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
