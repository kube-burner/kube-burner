#!/bin/bash

export QPS=5
export BURST=10
export TERM=screen-256color

bold=$(tput bold)
normal=$(tput sgr0)

log() {
    echo ${bold}$(date "+%d-%m-%YT%H:%M:%S") ${@}${normal}
}
