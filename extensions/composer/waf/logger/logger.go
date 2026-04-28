// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package logger provides logging functionality for the WAF plugin.
package logger

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
)

// Logger is an alias for zap.Logger
type Logger = zap.Logger

type logMode string

const (
	// ProductionLogMode uses production logging configuration
	ProductionLogMode logMode = "production"
	// DevelopmentLogMode uses development logging configuration
	DevelopmentLogMode logMode = "development"
)

var (
	mainLogger *Logger
	once       sync.Once
)

// TODO(wbpcode): may be we can remove this once the SDK provides the logger directly.
func initLogger() {
	isDebugMode := false
	if mode := os.Getenv("LOG_MODE"); mode != "" {
		switch logMode(mode) {
		case DevelopmentLogMode:
			isDebugMode = true
		default:
			isDebugMode = false
		}
	}

	var config zap.Config
	if isDebugMode {
		config = zap.NewDevelopmentConfig()
	} else {
		config = zap.NewProductionConfig()
	}

	var err error
	mainLogger, err = config.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to create mainLogger: %v", err))
	}
}

// GetLogger returns the main logger (for backward compatibility)
func GetLogger() *Logger {
	once.Do(initLogger)
	return mainLogger
}
