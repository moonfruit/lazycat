#!/usr/bin/env python3
import sys

if len(sys.argv) != 2:
    print(f"Usage: {sys.argv[0]} <input_file>", file=sys.stderr)
    sys.exit(1)

file_in = sys.argv[1]

insert_content = sys.stdin.read()

with open(file_in, "r") as fin:
    for line in fin:
        if "</title>" in line:
            parts = line.split("</title>", 1)
            sys.stdout.write(parts[0] + "</title>" + insert_content + parts[1])
        else:
            sys.stdout.write(line)
