# yada

Unbound DNS sunucularındaki yerel kayıtları SSH üzerinden yöneten komut satırı ve masaüstü uygulaması. Windows, Linux ve macOS üzerinde çalışır.

## Ne yapar

Bir veya birden fazla Unbound sunucusunda `local-zone` ve `local-data` kayıtlarını yönetir:

- Kayıt ekleme, listeleme, güncelleme ve silme (A, AAAA, CNAME, TXT, MX, PTR)
- CSV ile toplu içe ve dışa aktarma
- Sunucular arası fark gösterme ve eşitleme
- Yazmadan önce `unbound-checkconf` ile doğrulama, başarısızlıkta yedekten geri alma
- Kesintisiz yenileme: değişen kayıtları çalışan daemon'a `unbound-control local_data` ile yazar, olmazsa sırayla `reload_keep_cache`, `systemctl reload` ve `systemctl restart` dener

## Kurulum

Hazır binary'ler [Releases](https://github.com/KilimcininKorOglu/yada/releases) sayfasındadır. Yalnızca komut satırını isteyen bir sunucuya `yada-cli-<platform>` yeter; statiktir ve bağımlılık aramaz. `SHA256SUMS` dosyası aynı sayfadadır.

Komut satırı yapısı doğrudan Go ile de kurulur:

```bash
go install github.com/KilimcininKorOglu/yada/cmd/yada@latest
```

Kaynaktan derlemek için depoyu klonlayın. `go.mod` Go 1.26.5 ister.

```bash
git clone https://github.com/KilimcininKorOglu/yada.git
cd yada

make build            # yada (arayüz + CLI, cgo gerekir)
make build-cli        # yada-cli (yalnızca CLI, statik)
make cross-cli        # beş platform için statik CLI
make cross-gui        # beş platform için arayüzlü yapı (tek makineden)
```

`make build` tek bir binary üretir: argümansız çalıştırıldığında masaüstü arayüzünü açar, `-cli` ile komut satırına geçer. Fyne kullandığı için cgo ister ve her platformda ayrı derlenir. Linux'ta derlemek için `libgl1-mesa-dev` ve `xorg-dev` paketleri gerekir.

`make build-cli`, `nogui` build tag'i ile arayüzü çıkarır. Geriye `CGO_ENABLED=0` ile derlenen statik bir binary kalır; tek komutla beş platforma çapraz derlenir ve hedef makinede çalışma zamanı bağımlılığı aramaz. Sunucular ve CI için olan yapı budur.

`make cross-gui` beş platformun arayüzlü yapısını tek bir macOS makinesinden üretir. Hedef başına bir C toolchain ister:

```bash
brew install FiloSottile/musl-cross/musl-cross mingw-w64
```

Linux hedefleri çapraz derlenmez, konteyner içinde derlenir: glfw hedefin X11, Wayland ve OpenGL başlıklarına karşı derleniyor ve hiçbir çapraz toolchain bunları getirmiyor. Apple Silicon üzerinde `linux/amd64` emülasyonla çalışır, yavaştır. CI'nın bu hedefe ihtiyacı yok, her platformu kendi runner'ında derliyor.

## Ön koşullar

**Uygulamayı çalıştıran makinede:** `PATH` üzerinde bir `ssh` istemcisi. Arayüzdeki sunucu ekleme formu ayrıca aynı pakette gelen `ssh-keygen` ve `ssh-keyscan` araçlarını kullanır. Windows 10 sürüm 1809 ve sonrası OpenSSH istemcisiyle gelir; kapalıysa Ayarlar > Uygulamalar > İsteğe bağlı özellikler bölümünden eklenir.

Sistem `ssh` binary'si kullanılır, dolayısıyla `~/.ssh/config` dosyanız, ssh-agent ve `ProxyJump` gibi ayarlarınız olduğu gibi geçerlidir.

**Hedef sunucularda:** anahtar tabanlı SSH erişimi ve `$Username` için parolasız `sudo`. Bağlantı testi `BatchMode=yes` ile yapılır, bu yüzden parola isteyen sunucu beklemeye girmez, ulaşılamaz olarak raporlanır.

Kesintisiz yenileme kademeleri şunları ister:

| Kademe                              | Maliyet                                           | Gereksinim                                                                     |
|-------------------------------------|---------------------------------------------------|--------------------------------------------------------------------------------|
| `unbound-control local_data`        | Kesinti yok, config okunmaz, cache korunur        | `remote-control: control-enable: yes` ve `unbound-control-setup` sertifikaları |
| `unbound-control reload_keep_cache` | Kesinti yok, config yeniden okunur, cache korunur | Aynısı                                                                         |
| `systemctl reload`                  | Kesinti yok, cache temizlenir                     | Unit dosyasında `ExecReload` tanımlı olmalı                                    |
| `systemctl restart`                 | Kısa kesinti, cache temizlenir                    | Yok                                                                            |

İlk kademe değişen kayıtları çalışan daemon'a doğrudan yazar; kalıcılık, önce yazılan kayıt dosyasından gelir. Yalnızca bir yazma işleminin ardından kullanılabilir, çünkü neyin değiştiğinin bilinmesi gerekir. Tek başına `yada reload` bunu bilemez ve bir alt kademeden başlar.

Hangi kademenin kullanılabildiğini `yada check` gösterir.

## İlk kurulum

Yeni bir makinede en kolay yol masaüstü arayüzüdür. Argümansız çalıştırın, Sunucular sekmesinde **Sunucu ekle** düğmesine basın. Ayar dosyası hiç yoksa uygulama zaten açılışta bunu önerir.

Form adres, kullanıcı ve private key ister. Karşılığında dört şey yazılır:

1. Private key `~/.ssh/yada_<ad>` dosyasına, `0600` izinle
2. Sunucunun host key'i `~/.ssh/known_hosts` dosyasına
3. `~/.ssh/config` içine bir `Host` bloğu (`HostName`, `User`, `Port`, `IdentityFile`, `IdentitiesOnly`)
4. Sunucu `yada.conf` içindeki `servers` listesine

Host key yazılmadan önce parmak izi gösterilir ve onayınız istenir. Sunucunun yöneticisinden aldığınız parmak iziyle karşılaştırın. `known_hosts` dosyasında o adres için başka bir host key kayıtlıysa hiçbir şey yazılmaz: sunucu yeniden kurulduysa ilgili satırı elle silin, kurulmadıysa bağlantı beklediğiniz makineye gitmiyordur.

Public key'in sunucudaki `authorized_keys` dosyasında zaten olması gerekir. Uygulama parola ile bağlanmaz, dolayısıyla anahtarı sunucuya kuramaz.

Sunucular aynı anahtarı paylaşabilir. Yapıştırdığınız anahtar diskte zaten varsa ikinci bir kopya yazılmaz, mevcut dosya gösterilir.

`~/.ssh/config` ve `known_hosts` yazılmadan önce `.bak` kopyası alınır. Uygulama yalnızca kendi işaretleri (`# >>> yada <adres>`) arasındaki bölümü değiştirir; elle yazdığınız bloklara dokunmaz. Aynı adres için elle yazılmış bir `Host` bloğu varsa yazma yapılmaz ve durum bildirilir.

Windows'ta OpenSSH anahtar izinlerini ACL üzerinden denetler, `0600` orada aynı anlama gelmez. Uygulama anahtarı yazdıktan sonra çalıştırmanız gereken `icacls` komutunu gösterir.

## Ayar dosyası

`yada.conf` iki konumda aranır, ilk bulunan kullanılır:

1. Çalışan uygulamanın bulunduğu dizin
2. Kullanıcı ana dizini

`--config <yol>` her ikisini de geçersiz kılar. Örnek dosyayı oluşturmak için:

```bash
yada config init
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

Tüm anahtarların açıklaması proje kökündeki `yada.conf.example` dosyasındadır. Aynı dosya binary'nin içine gömülüdür, `config init` onu yazar.

`records_file`, `unbound.conf` içinden `include:` ile çağrılan dosyadır. `main_config` ise `unbound-checkconf` ile doğrulanan ana dosyadır. Kayıt dosyası tek başına doğrulanamaz, çünkü `include` onu `server:` bloğunun içine gömer ve fragment kendi başına geçerli bir config değildir.

## Kullanım

Argümansız çalıştırılınca masaüstü arayüzü açılır:

```bash
yada                               # arayüz
yada --config /yol/yada.conf  # arayüz, belirli bir ayar dosyasıyla
```

Komut satırı için `-cli` verin. Bir alt komut yazıldığında `-cli` gerekmez, çünkü alt komut zaten komut satırını seçer:

```bash
yada -cli check
yada check                         # aynısı

yada check                         # bağlantı ve config doğrulaması, değişiklik yapmaz
yada list                          # kayıtları listele
yada list --type A --filter google

yada add mail.google.com A 10.10.10.10
yada add web.local CNAME db.local. --ttl 3600
yada add example.com MX "10 mail.example.com."
yada add 10.10.10.10.in-addr.arpa PTR mail.google.com.

yada update mail.google.com --type A --value 10.20.30.40
yada delete eski.google.com

yada import kayitlar.csv
yada export kayitlar.csv

yada diff                          # sunucular arası fark
yada sync --from ns1               # ns1'i referans alarak eşitle
yada sync --from ns1 --prune       # fazladan kayıtları da sil

yada reload                        # yalnızca yenile
```

Genel bayraklar: `--config`, `--server` (tekrarlanabilir), `--json`, `--dry-run`, `--no-reload`, `--yes`.

`--dry-run` hiçbir şey yazmaz, yalnızca oluşacak farkı gösterir.

### Ad zaten kullanımdaysa

`add` yazmadan önce sunucuları okur ve girdiğiniz adın ne durumda olduğuna bakar:

| Durum | Davranış |
|---|---|
| Ad hiçbir sunucuda yok | Kaydedilir. Aynı adresin başka bir adda kullanılıyor olması engel değildir. |
| Ad var, değer birebir aynı | Yazma yapılmaz, durum bildirilir ve 0 ile çıkılır. |
| Ad var, değer farklı | Hangi sunucuda ne olduğu listelenir ve değiştirilmesi için onay istenir. |

Onay istendiğinde `--yes` sormadan devam eder, `--dry-run` sormadan yalnızca farkı gösterir. Terminal yoksa (script veya CI) komut yazmadan durur ve `--yes` ister, böylece bir otomasyon var olan kaydı kazara ezmez.

Onay verildiğinde tek karar bütün sunuculara uygulanır: kayıt eksik olan sunucuya eklenir, farklı olan sunucuda değiştirilir, zaten doğru olan sunucuda dokunulmaz.

Masaüstü arayüzü aynı denetimi yapar. Değer farklıysa "Düzenle" ve "Vazgeç" seçenekleri çıkar; "Düzenle" güncelleme penceresini açar, böylece son hâli yazılmadan önce görürsünüz.

### CSV biçimi

Başlık satırı zorunludur, sütun sırası serbesttir:

```csv
name,type,value,ttl
mail.google.com,A,10.10.10.10,
www.google.com,A,10.30.30.30,3600
ipv6.example.com,AAAA,2001:db8::1,
```

Hatalı satırlar satır numarasıyla bildirilir ve atlanır, geçerli satırlar uygulanır. Dışa aktarılan dosya doğrudan içe aktarılabilir.

Proje kökündeki `kayitlar.csv` her kayıt tipinden örnek içerir. Uygulamadan önce ne olacağını görmek için:

```bash
yada import kayitlar.csv --dry-run
```

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
cmd/yada/       giriş noktası, arayüz ve CLI arasında seçim yapar
internal/config/       ayar yükleme, doğrulama ve düzenleme
internal/transport/    sistem ssh sarmalayıcı
internal/sshsetup/     yerel anahtar, known_hosts ve ssh config yazma
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
