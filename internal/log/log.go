//
// log.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap.c@gmail.com>
//
// Distributed under terms of the MIT license.
//

package log

import (
	"fmt"
	"io"
	"log"
	"strings"
)

var verbose bool

func SetVerbose(enabled bool) {
	verbose = enabled
}

var indentLevel int = 0
var indent string
var newlineAfterUnindent = false

var logger *log.Logger

func Initialize(out io.Writer) {
	logger = log.New(out, "", log.LstdFlags|log.Lmsgprefix)
}

func IndentLevel() int {
	return indentLevel
}

func SetIndentLevel(level int) {
	if level != indentLevel {
		if level < indentLevel && newlineAfterUnindent {
			logger.Println("")
		}
		newlineAfterUnindent = false
	}
	indentLevel = level
	indent = strings.Repeat(" ", int(max(0, level))*4)
}

func Indent() {
	SetIndentLevel(indentLevel + 1)
}

func Unindent() {
	SetIndentLevel(indentLevel - 1)
}

func write(level string, format string, v ...any) {
	logger.SetPrefix(level + indent)
	logger.Printf(format+"\n", v...)
	newlineAfterUnindent = true
}

func Verbose(str string) {
	Verbosef(str)
}

func Verbosef(format string, v ...any) {
	if verbose {
		write("[V] ", format, v...)
		newlineAfterUnindent = true
	}
}

func Print(str string) {
	Printf(str)
}

func Printf(format string, v ...any) {
	Infof(format, v...)
}

func Info(str string) {
	Infof(str)
}

func Infof(format string, v ...any) {
	write("[I] ", format, v...)
}

func Warn(str string) {
	Warnf(str)
}

func Warnf(format string, v ...any) {
	write("[W] ", format, v...)
}

func Error(str string) {
	Errorf(str)
}

func Errorf(format string, v ...any) {
	write("[E] ", format, v...)
}

func Panic(str string) {
	Panicf(str)
}

func Panicf(format string, v ...any) {
	write("[F] ", format, v...)
	s := fmt.Sprintf(format, v...)
	panic(s)
}
