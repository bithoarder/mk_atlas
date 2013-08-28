#!/bin/sh
export GOPATH=$PWD
exec st3 --multiinstance --project .sublime-project --add &