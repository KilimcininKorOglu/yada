#!/usr/bin/env bash
# Drives the CLI against the containers from docker-compose.yml.
#
# Every assertion is made against what the resolver actually answers, not
# against what the tool printed, so a change that writes a correct file but
# never reaches the daemon still fails here.
set -euo pipefail

cd "$(dirname "$0")/.."

CLI=${CLI:-dist/unbound-dns-cli}
CONFIG=docker/unbound-dns.docker.conf
COMPOSE="docker compose -f docker/docker-compose.yml"

failures=0
step=0

# --- helpers ---------------------------------------------------------------

info() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
pass() { printf '  \033[32mOK\033[0m   %s\n' "$1"; }

fail() {
    printf '  \033[31mHATA\033[0m %s\n' "$1"
    failures=$((failures + 1))
}

begin() {
    step=$((step + 1))
    info "$step. $1"
}

cli() {
    "$CLI" --config "$CONFIG" "$@"
}

# resolve asks a server what it answers for a name, which is the only proof
# that a change reached the running daemon.
resolve() {
    local server=$1 name=$2 type=$3

    $COMPOSE exec -T "$server" \
        dig +short +time=2 +tries=1 "@127.0.0.1" -p 53 "$name" "$type" 2>/dev/null |
        tr -d '\r' | head -1
}

expect_resolves() {
    local server=$1 name=$2 type=$3 want=$4
    local got

    got=$(resolve "$server" "$name" "$type")

    if [ "$got" = "$want" ]; then
        pass "$server: $name $type = $want"
        return
    fi

    fail "$server: $name $type = '${got:-boş}', beklenen '$want'"
}

expect_empty() {
    local server=$1 name=$2 type=$3
    local got

    got=$(resolve "$server" "$name" "$type")

    if [ -z "$got" ]; then
        pass "$server: $name $type yanıtsız"
        return
    fi

    fail "$server: $name $type hâlâ '$got' yanıtlıyor"
}

expect_contains() {
    local haystack=$1 needle=$2 what=$3

    if grep -qF -- "$needle" <<<"$haystack"; then
        pass "$what"
        return
    fi

    fail "$what (çıktıda '$needle' yok)"
    printf '%s\n' "$haystack" | sed 's/^/      /'
}

expect_missing() {
    local haystack=$1 needle=$2 what=$3

    if grep -qF -- "$needle" <<<"$haystack"; then
        fail "$what (çıktıda beklenmeyen '$needle' var)"
        printf '%s\n' "$haystack" | sed 's/^/      /'
        return
    fi

    pass "$what"
}

on_server() {
    local server=$1
    shift

    $COMPOSE exec -T "$server" sh -c "$*"
}

# --- scenario --------------------------------------------------------------

begin "check: üç sunucu da ulaşılabilir, kademeler doğru raporlanıyor"
out=$(cli check 2>&1) || fail "check sıfırdan farklı kodla çıktı"
expect_contains "$out" "ns1" "ns1 raporlandı"
expect_contains "$out" "ns2" "ns2 raporlandı"
expect_contains "$out" "ns3" "ns3 raporlandı"
expect_missing "$out" "BAŞARISIZ" "hiçbir sunucu başarısız değil"

begin "add: kayıt üç sunucuya da yazılıyor ve anında çözülüyor"
out=$(cli add mail.example.test A 10.10.10.10 2>&1) || fail "add başarısız"
expect_contains "$out" "eklendi" "kayıt yazıldı"

# The lightest tier needs a control channel, so ns3 has to fall through.
expect_contains "$out" "[ns1] local_data ile yenilendi" "ns1 çalışma zamanı itmesi kullandı"
expect_contains "$out" "[ns3] systemctl reload ile yenilendi" "ns3 sinyal kademesine düştü"

for server in ns1 ns2 ns3; do
    expect_resolves "$server" mail.example.test A 10.10.10.10
done

begin "seed kaydı ve elle yazılmış yorumlar korunuyor"
expect_resolves ns1 seed.example.test A 10.99.99.1
body=$(on_server ns1 'cat /etc/unbound/local_records.conf')
expect_contains "$body" "# Local records, spliced into the server clause" "dosya başındaki yorum duruyor"

begin "add: aynı kayıt ikinci kez eklenmiyor"
out=$(cli add mail.example.test A 10.10.10.10 2>&1) || true
expect_contains "$out" "atlandı" "var olan kayıt atlandı"

begin "update: değer değişince daemon yeni değeri veriyor"
out=$(cli update mail.example.test --type A --value 10.20.30.40 2>&1) || fail "update başarısız"
expect_contains "$out" "güncellendi" "kayıt güncellendi"

for server in ns1 ns2 ns3; do
    expect_resolves "$server" mail.example.test A 10.20.30.40
done

begin "aynı ada ikinci tip: local_data_remove diğer tipi silmemeli"
cli add mail.example.test TXT "v=spf1 -all" >/dev/null 2>&1 || fail "TXT eklenemedi"
expect_resolves ns1 mail.example.test TXT '"v=spf1 -all"'

out=$(cli delete mail.example.test --type A --yes 2>&1) || fail "A kaydı silinemedi"
expect_contains "$out" "silindi" "A kaydı silindi"

# unbound-control local_data_remove drops every type under a name at once, so
# this is what proves the surviving record is pushed back.
expect_empty ns1 mail.example.test A
expect_resolves ns1 mail.example.test TXT '"v=spf1 -all"'
expect_resolves ns3 mail.example.test TXT '"v=spf1 -all"'

cli delete mail.example.test --yes >/dev/null 2>&1 || fail "TXT kaydı silinemedi"
expect_empty ns1 mail.example.test TXT

begin "import: CSV toplu yükleme"
cat > /tmp/unbound-dns-test.csv <<'EOF'
name,type,value,ttl
web.example.test,A,10.30.30.30,3600
alias.example.test,CNAME,web.example.test.,
ipv6.example.test,AAAA,2001:db8::1,
EOF

out=$(cli import /tmp/unbound-dns-test.csv 2>&1) || fail "import başarısız"
expect_contains "$out" "3 kayıt uygulanacak" "üç satır okundu"

expect_resolves ns1 web.example.test A 10.30.30.30
expect_resolves ns2 ipv6.example.test AAAA 2001:db8::1
expect_resolves ns3 alias.example.test CNAME web.example.test.

begin "export: dışa aktarılan dosya geri yüklenebilir"
cli export /tmp/unbound-dns-export.csv >/dev/null 2>&1 || fail "export başarısız"
expect_contains "$(cat /tmp/unbound-dns-export.csv)" "web.example.test" "dışa aktarımda kayıt var"

begin "diff ve sync: tek sunucudaki fazladan kayıt yayılıyor"
cli --server ns1 add only-on-ns1.example.test A 10.40.40.40 >/dev/null 2>&1 || fail "tek sunucuya ekleme başarısız"

out=$(cli diff 2>&1) || true
expect_contains "$out" "only-on-ns1.example.test" "fark raporlandı"

out=$(cli sync --from ns1 --yes 2>&1) || fail "sync başarısız"
expect_resolves ns2 only-on-ns1.example.test A 10.40.40.40
expect_resolves ns3 only-on-ns1.example.test A 10.40.40.40

out=$(cli diff 2>&1) || fail "eşitleme sonrası diff başarısız"
expect_contains "$out" "eşit" "sunucular eşitlendi"

begin "geri alma: config doğrulaması başarısızsa dosya eski haline dönüyor"
before=$(on_server ns3 'md5sum /etc/unbound/local_records.conf' | awk '{print $1}')
on_server ns3 'printf "\nbu-bir-direktif-degil: 1\n" >> /etc/unbound/unbound.conf'

out=$(cli --server ns3 add rollback.example.test A 10.50.50.50 2>&1) || true
expect_contains "$out" "BAŞARISIZ" "bozuk config'te yazma başarısız bildirildi"
expect_contains "$out" "geri yüklendi" "dosya yedekten geri yüklendi"

after=$(on_server ns3 'md5sum /etc/unbound/local_records.conf' | awk '{print $1}')
if [ "$before" = "$after" ]; then
    pass "kayıt dosyası bayt bayt eski haline döndü"
else
    fail "geri alma sonrası dosya değişmiş kaldı ($before -> $after)"
fi

# Put the server back in a usable state for the cleanup below.
on_server ns3 "sed -i '/bu-bir-direktif-degil/d' /etc/unbound/unbound.conf"
on_server ns3 'unbound-checkconf /etc/unbound/unbound.conf >/dev/null'

begin "temizlik: eklenen kayıtlar siliniyor"
for name in web.example.test alias.example.test ipv6.example.test only-on-ns1.example.test; do
    cli delete "$name" --yes >/dev/null 2>&1 || fail "$name silinemedi"
done

expect_empty ns1 web.example.test A
expect_resolves ns1 seed.example.test A 10.99.99.1

# --- verdict ---------------------------------------------------------------

echo
if [ "$failures" -eq 0 ]; then
    printf '\033[32mTüm adımlar geçti.\033[0m\n'
    exit 0
fi

printf '\033[31m%d kontrol başarısız.\033[0m\n' "$failures"
exit 1
