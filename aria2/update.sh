#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/fs.sh"
source "$ENV/lib/bash/github.sh"

echo " --- === Updating ariang === ---"
if [[ -f ariang.url ]]; then
    OLD=$(cat ariang.url)
fi
download-latest-release dist/ariang mayswind AriaNg 'test("AriaNg-\\d+(\\.\\d+)+\\.zip")' ariang.url
NEW=$(cat ariang.url)
echo "Using url: $NEW"
if [[ $OLD != "$NEW" ]]; then
    create-temp-file TEMP
    # shellcheck disable=SC2153
    echo -n "<script>
(function () {
    const options = localStorage.getItem('AriaNg.Options');
    if (!options) {
        window.location.href = '/#!/settings/rpc/set'
                + '?protocol=wss'
                + '&host=aria2.dkmooncat.heiyu.space'
                + '&port=443'
                + '&interface=jsonrpc'
                + '&secret=YXJpYTI';
    }
})();
</script>" | ./hack.py dist/ariang/index.html >"$TEMP"
    mv "$TEMP" dist/ariang/index.html
fi
echo
