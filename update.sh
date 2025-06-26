#!/usr/bin/env bash
fd -tf update.sh | while read -r MODULE; do
    if [[ $MODULE == update.sh ]]; then
        continue
    fi
    DIR=${MODULE%/*}
    echo "-------- $DIR --------"
    (cd "$DIR" && ./update.sh -N)
done

echo "-------- git diff --------"
git diff
