#!/usr/bin/env bash
# seq 服务入口（lzc-manifest: entrypoint: "" + command: bash /pkg/run.sh）。
#
# 懒猫每次安装/升级都会给 dex 重新注册 client 并轮换 client_secret，而 Seq 只在进程
# 启动时把 OIDC 配置读进内存（日志 `Enabling OpenID Connect authentication`），此后
# 写设置不再重读；`/lzcapp/var` 里留的又是上一轮的 secret。若等正式实例起来再写，
# 它整个生命周期都会拿旧 secret 去换 token，dex 一律回 invalid_client。
#
# 所以在拉起正式实例之前先把设置写好：
#   指纹一致（日常重启）      -> 直接 exec seqentry，与不加这层包装时完全一致
#   指纹变化（升级/首次安装） -> 起一个只听 localhost 高位端口的临时实例写入设置，
#                               停掉后再 exec seqentry，正式实例一启动就是新 secret
# OIDC 只能作为 runtime setting 经 HTTP API 写入（seqsvr config 里没有任何 OIDC 键），
# 因此这一步绕不开一个跑着的 Seq。
#
# API key 自举顺序：
#   1. 全新实例首启处于无认证窗口（SEQ_FIRSTRUN_NOAUTHENTICATION=true），匿名创建；
#      此时会一并删除内置 admin 用户——免费 Individual 许可只允许一个用户，
#      把名额留给 OIDC 首次登录时按 email（uid@盒域名）自动创建的管理员账户。
#   2. 已初始化为本地认证的旧实例，用 SEQ_SETUP_ADMINPASSWORD 提供的管理员口令创建；
#      此场景不删除 admin，请手动将其用户名改为 uid@盒域名 以便 OIDC 关联。
# 切换到 OIDC 后本地口令登录即失效，恢复途径：容器内执行 `seqsvr auth reset`。

TOKEN_FILE=/data/.lzc-setup-token
FINGERPRINT_FILE=/data/.lzc-oidc.sha256
SETUP_URI=http://localhost:18080
# Seq 在被列为 ingestion 的端口上只暴露 ingestion 路由（/api 会 404），
# 故另给一个本机端口承接 ingestion，让 SETUP_URI 保持完整 API
SETUP_INGEST_URI=http://localhost:18081

log() {
    echo "[lzc-setup] $*"
}

# 三个值任一变化都要重配；secret 在 Seq 里是加密存储且 `seqcli setting show` 恒返回
# null（掩码），无法读回比对，只能自己记指纹
fingerprint() {
    printf '%s\n%s\n%s' \
        "$LAZYCAT_AUTH_OIDC_ISSUER_URI" \
        "$LAZYCAT_AUTH_OIDC_CLIENT_ID" \
        "$LAZYCAT_AUTH_OIDC_CLIENT_SECRET" |
        sha256sum | cut -d' ' -f1
}

wait-for-api() {
    for _ in {1..90}; do
        kill -0 "$SETUP_PID" 2>/dev/null || return 1
        if curl -fso /dev/null "$SETUP_URI/api"; then
            return 0
        fi
        sleep 2
    done
    return 1
}

bootstrap-token() {
    local args=(-t "LazyCat OIDC Setup" -s "$SETUP_URI"
        --permissions Ingest,Read,Write,Project,Organization,System)
    umask 077
    if seqcli apikey create "${args[@]}" >"$TOKEN_FILE" 2>/dev/null; then
        # 匿名创建成功说明处于首启无认证窗口：释放内置 admin 占用的用户名额
        seqcli user remove -i user-admin -s "$SETUP_URI" 2>/dev/null &&
            log "已删除内置 admin 用户，等待 OIDC 首次登录自动建号"
        return 0
    fi
    if [[ -n $SEQ_SETUP_ADMINPASSWORD ]]; then
        seqcli apikey create "${args[@]}" \
            --connect-username admin \
            --connect-password "$SEQ_SETUP_ADMINPASSWORD" >"$TOKEN_FILE" && return 0
    fi
    rm -f "$TOKEN_FILE"
    return 1
}

# 与现值一致则跳过：重复 PUT AuthenticationProvider 会解绑既有用户身份
ensure-setting() {
    local auth=(-s "$SETUP_URI" -a "$(<"$TOKEN_FILE")")
    if [[ $(seqcli setting show "${auth[@]}" -n "$1" 2>/dev/null) != "$2" ]]; then
        seqcli setting set "${auth[@]}" -n "$1" --value-stdin <<<"$2" &&
            log "已更新 $1"
    fi
}

configure() {
    if ! wait-for-api; then
        log "等待临时实例 API 就绪超时"
        return 1
    fi
    if [[ ! -s $TOKEN_FILE ]] && ! bootstrap-token; then
        log "无法创建 API key"
        return 1
    fi
    ensure-setting OpenIdConnectAuthority "$LAZYCAT_AUTH_OIDC_ISSUER_URI" &&
        ensure-setting OpenIdConnectClientId "$LAZYCAT_AUTH_OIDC_CLIENT_ID" &&
        ensure-setting OpenIdConnectClientSecret "$LAZYCAT_AUTH_OIDC_CLIENT_SECRET" &&
        ensure-setting OpenIdConnectScopes "openid,profile,email" &&
        ensure-setting AutomaticallyProvisionAuthenticatedUsers True &&
        ensure-setting NewUserRoleIds role-administrator &&
        ensure-setting AuthenticationProvider "OpenID Connect" &&
        ensure-setting IsAuthenticationEnabled True
}

if [[ -z $LAZYCAT_AUTH_OIDC_CLIENT_ID || -z $LAZYCAT_AUTH_OIDC_CLIENT_SECRET || -z $LAZYCAT_AUTH_OIDC_ISSUER_URI ]]; then
    log "未注入 OIDC 环境变量，跳过认证配置"
    exec seqentry "$@"
fi

WANT=$(fingerprint)
if [[ -f $FINGERPRINT_FILE && $(<"$FINGERPRINT_FILE") == "$WANT" ]]; then
    exec seqentry "$@"
fi

log "OIDC 配置有变，先由临时实例写入，避免正式实例沿用旧 secret"
# seqinit 每次启动都会把 api.listenUris/ingestionPorts 重置回默认，故必须在它之后收窄；
# 临时实例只听本机高位端口，lzcinit 代理与 gelf 都连不上，配置期间外部无法访问。
# 正式实例 exec seqentry 时 seqinit 会再跑一次，把两项重置回默认，无需手动还原。
seqinit "$@"
seqsvr config set -k api.listenUris -v "$SETUP_URI,$SETUP_INGEST_URI"
seqsvr config set -k api.ingestionPorts -v "${SETUP_INGEST_URI##*:}"
seqsvr run &
SETUP_PID=$!

if configure; then
    printf '%s' "$WANT" >"$FINGERPRINT_FILE"
    log "OIDC 认证配置完成"
else
    log "OIDC 认证配置失败，仍以现有设置启动"
fi

# 必须等临时实例完全退出，正式实例才拿得到事件存储的文件锁
kill "$SETUP_PID" 2>/dev/null
wait "$SETUP_PID" 2>/dev/null

log "临时实例已停止，启动正式实例"
exec seqentry "$@"
