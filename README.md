# unbound-dns

Unbound DNS sunucularındaki yerel kayıtları SSH üzerinden yöneten komut satırı ve masaüstü uygulaması. Windows, Linux ve macOS üzerinde çalışır.

## Ne yapar

Bir veya birden fazla Unbound sunucusunda `local-zone` ve `local-data` kayıtlarını yönetir:

- Kayıt ekleme, listeleme, güncelleme ve silme (A, AAAA, CNAME, TXT, MX, PTR)
- CSV ile toplu içe ve dışa aktarma
- Sunucular arası fark gösterme ve eşitleme
- Yazmadan önce `unbound-checkconf` ile doğrulama, başarısızlıkta yedekten geri alma
- Kesintisiz yenileme: `unbound-control reload_keep_cache`, olmazsa `systemctl reload`, o da olmazsa `systemctl restart`

## Kurulum

Hazır binary yoksa kaynaktan derleyin. Go 1.24 veya üstü gerekir.

```bash
make build-cli        # unbound-dns (saf Go, cgo yok)
make build-gui        # unbound-dns-gui (Fyne, cgo gerekir)
make cross-cli        # linux, macOS ve Windows için CLI
```

CLI `CGO_ENABLED=0` ile derlenir, bu yüzden tek komutla beş platforma çapraz derlenir ve hedef makinede çalışma zamanı bağımlılığı aramaz.

GUI, Fyne kullandığı için cgo ister ve her platformda ayrı derlenir. Linux'ta derlemek için `libgl1-mesa-dev` ve `xorg-dev` paketleri gerekir.

## Ön koşullar

**Uygulamayı çalıştıran makinede:** `PATH` üzerinde bir `ssh` istemcisi. Windows 10 sürüm 1809 ve sonrası OpenSSH istemcisiyle gelir; kapalıysa Ayarlar > Uygulamalar > İsteğe bağlı özellikler bölümünden eklenir.

Sistem `ssh` binary'si kullanılır, dolayısıyla `~/.ssh/config` dosyanız, ssh-agent ve `ProxyJump` gibi ayarlarınız olduğu gibi geçerlidir.

**Hedef sunucularda:** anahtar tabanlı SSH erişimi ve `$Username` için parolasız `sudo`. Bağlantı testi `BatchMode=yes` ile yapılır, bu yüzden parola isteyen sunucu beklemeye girmez, ulaşılamaz olarak raporlanır.

Kesintisiz yenileme kademeleri şunları ister:

| Kademe | Gereksinim |
|---|---|
| `unbound-control reload_keep_cache` | `unbound.conf` içinde `remote-control: control-enable: yes` ve `unbound-control-setup` ile üretilmiş sertifikalar |
| `systemctl reload` | Unit dosyasında `ExecReload` tanımlı olmalı |
| `systemctl restart` | Yok |

Hangi kademenin kullanılabildiğini `unbound-dns check` gösterir.

## Ayar dosyası

`unbound-dns.conf` iki konumda aranır, ilk bulunan kullanılır:

1. Çalışan uygulamanın bulunduğu dizin
2. Kullanıcı ana dizini

`--config <yol>` her ikisini de geçersiz kılar. Örnek dosyayı oluşturmak için:

```bash
unbound-dns config init
```

En küçük geçerli ayar:

```yaml
servers:
  - name: ns1
    host: 192.0.2.4
  - name: ns2
    host: 192.0.2.5

defaults:
  user: user01
  records_file: /etc/unbound/local_records.conf
  main_config: /etc/unbound/unbound.conf
```

Tüm anahtarların açıklaması `internal/config/unbound-dns.conf.example` dosyasındadır.

`records_file`, `unbound.conf` içinden `include:` ile çağrılan dosyadır. `main_config` ise `unbound-checkconf` ile doğrulanan ana dosyadır. Kayıt dosyası tek başına doğrulanamaz, çünkü `include` onu `server:` bloğunun içine gömer ve fragment kendi başına geçerli bir config değildir.

## Kullanım

```bash
unbound-dns check                         # bağlantı ve config doğrulaması, değişiklik yapmaz
unbound-dns list                          # kayıtları listele
unbound-dns list --type A --filter google

unbound-dns add mail.google.com A 10.10.10.10
unbound-dns add web.local CNAME db.local. --ttl 3600
unbound-dns add example.com MX "10 mail.example.com."
unbound-dns add 10.10.10.10.in-addr.arpa PTR mail.google.com.

unbound-dns update mail.google.com --type A --value 10.20.30.40
unbound-dns delete eski.google.com

unbound-dns import kayitlar.csv
unbound-dns export kayitlar.csv

unbound-dns diff                          # sunucular arası fark
unbound-dns sync --from ns1               # ns1'i referans alarak eşitle
unbound-dns sync --from ns1 --prune       # fazladan kayıtları da sil

unbound-dns reload                        # yalnızca yenile
```

Genel bayraklar: `--config`, `--server` (tekrarlanabilir), `--json`, `--dry-run`, `--no-reload`, `--yes`.

`--dry-run` hiçbir şey yazmaz, yalnızca oluşacak farkı gösterir.

Masaüstü arayüzü için `unbound-dns-gui` çalıştırın.

### CSV biçimi

Başlık satırı zorunludur, sütun sırası serbesttir:

```csv
name,type,value,ttl
mail.google.com,A,10.10.10.10,
www.google.com,A,10.30.30.30,3600
ipv6.example.com,AAAA,2001:db8::1,
```

Hatalı satırlar satır numarasıyla bildirilir ve atlanır, geçerli satırlar uygulanır. Dışa aktarılan dosya doğrudan içe aktarılabilir.

## Yazma güvenliği

Her yazma şu sırayla ilerler:

1. Kayıt dosyasını oku ve ayrıştır
2. Değişikliği bellekte uygula
3. `.bak` uzantılı yedek al
4. Yeni içeriği `tee` komutuna stdin üzerinden yaz
5. `unbound-checkconf` ile ana config'i doğrula
6. Doğrulama başarısızsa yedeği geri yükle ve çıktıyı göster
7. Yalnızca doğrulama geçerse servisi yenile

Kayıt içeriği hiçbir zaman uzak komut satırına gömülmez; stdin üzerinden gider, böylece kayıtlardaki tırnak ve noktalı virgül karakterleri uzak kabuk tarafından yorumlanmaz.

Dosyada yönetilmeyen satırlar (yorumlar, boş satırlar, tanınmayan direktifler) ham metin olarak saklanır ve aynen geri yazılır. Elle eklenmiş yorumlarınız silme veya güncelleme sırasında kaybolmaz.

## Çıkış kodları

| Kod | Anlamı |
|---|---|
| 0 | Başarılı |
| 1 | Genel hata |
| 2 | Ayar hatası |
| 3 | Bağlantı hatası |
| 4 | Config doğrulama hatası |
| 5 | Kısmi başarı (bazı sunucular veya satırlar başarısız) |

## Geliştirme

```bash
make test        # go test -count=1 ./...
make test-race
make lint        # golangci-lint
make vuln        # govulncheck
```

Testler önbelleksiz çalışır. Uzak işlemler, `PATH` başına konan sahte bir `ssh` betiği ile sınanır; betik uzak komutu yerel kabukta çalıştırdığı için kabuk alıntılama ve dosya yazma gerçekten test edilir.

### Yapı

```
cmd/unbound-dns/       CLI
cmd/unbound-dns-gui/   Fyne arayüzü
internal/config/       ayar yükleme ve doğrulama
internal/transport/    sistem ssh sarmalayıcı
internal/records/      kayıt modeli, ayrıştırma, serileştirme
internal/unbound/      okuma, yazma, doğrulama, yenileme
internal/diff/         sunucular arası karşılaştırma
internal/bulk/         CSV
internal/ui/           Fyne ekranları
```

İş mantığının tamamı `internal/` altındadır. CLI ve GUI aynı paketleri çağırır, davranışı tekrarlamaz.

## unbound.ps1

Depodaki PowerShell scripti bu uygulamanın öncülüdür ve çalışır durumda tutulmaktadır. Yalnızca A kaydı ekler, listeleme ve silme yapmaz.
