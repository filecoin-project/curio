#!/bin/bash

#OP: Only run if some file in ../* is newer than catalog.go
# Change this condition if using translations elsewhere.
if [ "$(find ../../* -newer catalog.go)" ]; then
  gotext -srclang=en update -out=catalog.go -lang=en,zh,ko github.com/filecoin-project/curio/cmd/curio/
  go run knowns/main.go ./locales/zh ./locales/ko
fi
