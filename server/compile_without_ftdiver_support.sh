#!/bin/sh
TARGET=diverdriver
cpp -P $TARGET.pgo $TARGET.go | go build $TARGET.go
