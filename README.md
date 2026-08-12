# unbound-dns

Unbound DNS sunucularındaki yerel kayıtları SSH üzerinden yöneten komut satırı ve masaüstü uygulaması. Windows, Linux ve macOS üzerinde çalışır.

## Ne yapar

Bir veya birden fazla Unbound sunucusunda `local-zone` ve `local-data` kayıtlarını yönetir:

- Kayıt ekleme, listeleme, güncelleme ve silme (A, AAAA, CNAME, TXT, MX, PTR)
- CSV ile toplu içe ve dışa aktarma
- Sunucular arası fark gösterme ve eşitleme
- Yazmadan önce `unbound-checkconf` ile doğrulama, başarısızlıkta yedekten geri alma
- Kesintisiz yenileme: değişen kayıtları çalışan daemon'a `unbound-control local_data` ile yazar, olmazsa sırayla `reload_keep_cache`, `systemctl reload` ve `systemctl restart` dener

## Kurulum

Hazır binary yoksa kaynaktan derleyin. `go.mod` Go 1.26.5 ister.

```bash
make build            # unbound-dns (arayüz + CLI, cgo gerekir)
make build-cli        # unbound-dns-cli (yalnızca CLI, statik)
make cross-cli        # linux, macOS ve Windows için statik CLI
```

`make build` tek bir binary üretir: argümansız çalıştırıldığında masaüstü arayüzünü açar, `-cli` ile komut satırına geçer. Fyne kullandığı için cgo ister ve her platformda ayrı derlenir. Linux'ta derlemek için `libgl1-mesa-dev` ve `xorg-dev` paketleri gerekir.

`make build-cli`, `nogui` build tag'i ile arayüzü çıkarır. Geriye `CGO_ENABLED=0` ile derlenen statik bir binary kalır; tek komutla beş platforma çapraz derlenir ve hedef makinede çalışma zamanı bağımlılığı aramaz. Sunucular ve CI için olan yapı budur.

## Ön koşullar

**Uygulamayı çalıştıran makinede:** `PATH` üzerinde bir `ssh` istemcisi. Windows 10 sürüm 1809 ve sonrası OpenSSH istemcisiyle gelir; kapalıysa Ayarlar > Uygulamalar > İsteğe bağlı özellikler bölümünden eklenir.

Sistem `ssh` binary'si kullanılır, dolayısıyla `~/.ssh/config` dosyanız, ssh-agent ve `ProxyJump` gibi ayarlarınız olduğu gibi geçerlidir.

**Hedef sunucularda:** anahtar tabanlı SSH erişimi ve `$Username` için parolasız `sudo`. Bağlantı testi `BatchMode=yes` ile yapılır, bu yüzden parola isteyen sunucu beklemeye girmez, ulaşılamaz olarak raporlanır.

Kesintisiz yenileme kademeleri şunları ister:

| Kademe                              | Maliyet                                           | Gereksinim                                                                     |
|-------------------------------------|---------------------------------------------------|--------------------------------------------------------------------------------|
| `unbound-control local_data`        | Kesinti yok, config okunmaz, cache korunur        | `remote-control: control-enable: yes` ve `unbound-control-setup` sertifikaları |
| `unbound-control reload_keep_cache` | Kesinti yok, config yeniden okunur, cache korunur | Aynısı                                                                         |
| `systemctl reload`                  | Kesinti yok, cache temizlenir                     | Unit dosyasında `ExecReload` tanımlı olmalı                                    |
| `systemctl restart`                 | Kısa kesinti, cache temizlenir                    | Yok                                                                            |

İlk kademe değişen kayıtları çalışan daemon'a doğrudan yazar; kalıcılık, önce yazılan kayıt dosyasından gelir. Yalnızca bir yazma işleminin ardından kullanılabilir, çünkü neyin değiştiğinin bilinmesi gerekir. Tek başına `unbound-dns reload` bunu bilemez ve bir alt kademeden başlar.

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

Argümansız çalıştırılınca masaüstü arayüzü açılır:

```bash
unbound-dns                               # arayüz
unbound-dns --config /yol/unbound-dns.conf  # arayüz, belirli bir ayar dosyasıyla
```

Komut satırı için `-cli` verin. Bir alt komut yazıldığında `-cli` gerekmez, çünkü alt komut zaten komut satırını seçer:

```bash
unbound-dns -cli check
unbound-dns check                         # aynısı

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

| Kod | Anlamı                                                |
|-----|-------------------------------------------------------|
| 0   | Başarılı                                              |
| 1   | Genel hata                                            |
| 2   | Ayar hatası                                           |
| 3   | Bağlantı hatası                                       |
| 4   | Config doğrulama hatası                               |
| 5   | Kısmi başarı (bazı sunucular veya satırlar başarısız) |

## Geliştirme

```bash
make test        # go test -count=1 ./...
make test-race
make test-nogui  # arayüz paketi olmadan, OpenGL başlıkları gerekmez
make lint        # golangci-lint
make vuln        # govulncheck
```

Testler önbelleksiz çalışır. Uzak işlemler, `PATH` başına konan sahte bir `ssh` betiği ile sınanır; betik uzak komutu yerel kabukta çalıştırdığı için kabuk alıntılama ve dosya yazma gerçekten test edilir.

Bunun üstünde, SSH erişimli üç gerçek Unbound sunucusundan oluşan bir Docker ortamı vardır:

```bash
make docker-test     # ayağa kaldır ve uçtan uca senaryoyu çalıştır
make docker-down
```

Doğrulamalar aracın çıktısına değil, resolver'ın verdiği yanıta bakar. Ayrıntılar için `docker/README.md`.

### Yapı

```
cmd/unbound-dns/       giriş noktası, arayüz ve CLI arasında seçim yapar
internal/config/       ayar yükleme ve doğrulama
internal/transport/    sistem ssh sarmalayıcı
internal/records/      kayıt modeli, ayrıştırma, serileştirme
internal/unbound/      okuma, yazma, doğrulama, yenileme
internal/diff/         sunucular arası karşılaştırma
internal/bulk/         CSV
internal/ui/           Fyne ekranları
```

İş mantığının tamamı `internal/` altındadır. CLI ve GUI aynı paketleri çağırır, davranışı tekrarlamaz.

`internal/ui` paketi `nogui` build tag'i ile tamamen dışarıda kalır, bu yüzden statik CLI yapısı OpenGL başlıkları olmayan bir makinede de derlenir.

### Sürekli entegrasyon

`.github/workflows/ci.yml` her push ve pull request'te çalışır: biçim, `go mod tidy` denetimi, `go vet`, testler (yarış dedektörüyle birlikte), her iki build varyantı için lint, `govulncheck`, beş platforma çapraz derleme, üç işletim sisteminde arayüzlü derleme ve Docker ortamında uçtan uca senaryo.

`.github/workflows/release.yml` `v*` biçiminde bir etiket atıldığında binary'leri üretir, `SHA256SUMS` hesaplar ve GitHub release'i oluşturur.

## unbound.ps1

Depodaki PowerShell scripti bu uygulamanın öncülüdür ve çalışır durumda tutulmaktadır. Yalnızca A kaydı ekler, listeleme ve silme yapmaz.
