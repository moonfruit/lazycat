#!/bin/bash
# lzc setup_script：对接懒猫 OIDC。
# 自身后台化，等待 Seq API 就绪后，用保存在 /data 的 API key
# 幂等地核对认证设置（可容忍懒猫重装导致的 client_secret 轮换）。
#
# API key 自举顺序：
#   1. 全新实例首启处于无认证窗口（SEQ_FIRSTRUN_NOAUTHENTICATION=true），匿名创建；
#      此时会一并删除内置 admin 用户——免费 Individual 许可只允许一个用户，
#      把名额留给 OIDC 首次登录时按 email（uid@盒域名）自动创建的管理员账户。
#   2. 已初始化为本地认证的旧实例，用 SEQ_SETUP_ADMINPASSWORD 提供的管理员口令创建；
#      此场景不删除 admin，请手动将其用户名改为 uid@盒域名 以便 OIDC 关联。
# 切换到 OIDC 后本地口令登录即失效，恢复途径：容器内执行 `seqsvr auth reset`。

TOKEN_FILE=/data/.lzc-setup-token
LOG_FILE=/data/.lzc-setup.log
SERVER=http://localhost

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

wait-for-api() {
    for _ in {1..90}; do
        if curl -fso /dev/null "$SERVER/api"; then
            return 0
        fi
        sleep 2
    done
    return 1
}

bootstrap-token() {
    local args=(-t "LazyCat OIDC Setup" -s "$SERVER"
        --permissions Ingest,Read,Write,Project,Organization,System)
    umask 077
    if seqcli apikey create "${args[@]}" >"$TOKEN_FILE" 2>/dev/null; then
        # 匿名创建成功说明处于首启无认证窗口：释放内置 admin 占用的用户名额
        seqcli user remove -i user-admin -s "$SERVER" 2>/dev/null &&
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
    local auth=(-s "$SERVER" -a "$(<"$TOKEN_FILE")")
    if [[ $(seqcli setting show "${auth[@]}" -n "$1" 2>/dev/null) != "$2" ]]; then
        seqcli setting set "${auth[@]}" -n "$1" --value-stdin <<<"$2" &&
            log "已更新 $1"
    fi
}

configure() {
    if [[ -z $LAZYCAT_AUTH_OIDC_CLIENT_ID || -z $LAZYCAT_AUTH_OIDC_CLIENT_SECRET || -z $LAZYCAT_AUTH_OIDC_ISSUER_URI ]]; then
        log "未注入 OIDC 环境变量，跳过认证配置"
        return 0
    fi
    if ! wait-for-api; then
        log "等待 Seq API 就绪超时"
        return 1
    fi
    if [[ ! -s $TOKEN_FILE ]] && ! bootstrap-token; then
        log "无法创建 API key，跳过认证配置"
        return 1
    fi
    ensure-setting OpenIdConnectAuthority "$LAZYCAT_AUTH_OIDC_ISSUER_URI" &&
        ensure-setting OpenIdConnectClientId "$LAZYCAT_AUTH_OIDC_CLIENT_ID" &&
        ensure-setting OpenIdConnectClientSecret "$LAZYCAT_AUTH_OIDC_CLIENT_SECRET" &&
        ensure-setting OpenIdConnectScopes "openid,profile,email" &&
        ensure-setting AutomaticallyProvisionAuthenticatedUsers True &&
        ensure-setting NewUserRoleIds role-administrator &&
        ensure-setting AuthenticationProvider "OpenID Connect" &&
        ensure-setting IsAuthenticationEnabled True &&
        log "OIDC 认证配置核对完成"
}

if [[ $1 == "--configure" ]]; then
    configure
else
    nohup bash "$0" --configure >>"$LOG_FILE" 2>&1 &
fi
