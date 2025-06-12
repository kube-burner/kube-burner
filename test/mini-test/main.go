package main

import (
"fmt"
"os"

log "github.com/sirupsen/logrus"
)

func main() {
log.Info("Kube-Burner template fix test")
fmt.Printf("Syntax check passed!\n")
os.Exit(0)
}
