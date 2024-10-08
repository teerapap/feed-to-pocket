//
// util.go
// Copyright (C) 2024 Teerapap Changwichukarn <teerapap.c@gmail.com>
//
// Distributed under terms of the MIT license.
//

package util

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
)

const AppVersion = "v0.3.0"

func Must(err error) func(doing string) {
	return func(doing string) {
		if err != nil {
			panic(fmt.Errorf("while %s: %w", doing, err))
		}
	}
}

func Must1[T any](obj T, err error) func(doing string) T {
	return func(doing string) T {
		Must(err)(doing)
		return obj
	}
}

func Must2[T1 any, T2 any](obj1 T1, obj2 T2, err error) func(doing string) (T1, T2) {
	return func(doing string) (T1, T2) {
		Must(err)(doing)
		return obj1, obj2
	}
}

func Assert(cond bool, errMsg string) {
	if !cond {
		panic(errors.New(errMsg))
	}
}

func CreateWorkDir(path *string, clean bool) (bool, error) {
	if strings.TrimSpace(*path) == "" {
		// create temp directory
		tmpDir, err := os.MkdirTemp("", "mangafmt-")
		if err != nil {
			return true, fmt.Errorf("creating temp directory: %w", err)
		}
		*path = tmpDir
		return true, nil
	} else {
		if clean {
			// clean existing directory
			err := os.RemoveAll(*path)
			if err != nil {
				return false, fmt.Errorf("removing existing work directory: %w", err)
			}
		}
		err := os.MkdirAll(*path, 0750)
		if err != nil {
			return false, err
		}
		return false, nil
	}
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
