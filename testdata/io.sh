#! /bin/bash -
STR=
input()
{
    local a=$1; if [ "$a" == "-" ]; then read a; fi
    STR=$a
    echo $a
}

input $1
>&2 echo $STR
