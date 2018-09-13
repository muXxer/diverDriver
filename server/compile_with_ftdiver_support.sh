#!/bin/sh
TARGET=diverdriver
go get "github.com/muxxer/ftdiver"
cpp -DFTDIVER -P $TARGET.pgo $TARGET.go | go build $TARGET.go
