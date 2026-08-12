# yada

Unbound DNS sunucularındaki yerel kayıtları SSH üzerinden yöneten komut satırı ve masaüstü uygulaması. Windows, Linux ve macOS üzerinde çalışır.

## Ne yapar

Bir veya birden fazla Unbound sunucusunda `local-zone` ve `local-data` kayıtlarını yönetir:

- Kayıt ekleme, listeleme, güncelleme ve silme (A, AAAA, CNAME, TXT, MX, PTR)
- CSV ile toplu içe ve dışa aktarma
- Sunucular arası fark gösterme ve eşitleme
- Yazmadan önce `unbound-checkconf` ile doğrulama, başarısızlıkta yedekten geri alma
- Kesintisiz yenileme: değişen kayıtları çalışan daemon'a `unbound-control local_data` ile yazar, olmazsa sırayla `reload_keep_cache`, `systemctl reload` ve `systemctl restart` dener
- Sunucuyu arayüzden ekleme: private key dosyasını, `known_hosts` girdisini ve `~/.ssh/config` bloğunu kendisi yazar

Tek bir binary hem arayüzü hem komut satırını içerir. Argümansız çalıştırılınca pencere açılır, `-cli` veya bir alt komut verilince komut satırı çalışır.

## Kurulum

Hazır binary'ler [Releases](https://github.com/KilimcininKorOglu/yada/releases) sayfasındadır. İki tür dosya vardır:

| Dosya                  | İçerik                                                                                                           |
|------------------------|------------------------------------------------------------------------------------------------------------------|
| `yada-<os>-<arch>`     | Yalnızca komut satırı. Statiktir, hedef makinede bağımlılık aramaz. Sunucular ve otomasyon için olan yapı budur. |
| `yada-gui-<os>-<arch>` | Arayüzü de içerir. Argümansız çalıştırıldığında pencereyi açar, `-cli` ile komut satırına geçer.                 |

`SHA256SUMS` aynı sayfadadır. İndirdikten sonra doğrulayın:

```bash
sha256sum -c SHA256SUMS --ignore-missing
chmod +x yada-linux-amd64
```

Komut satırı yapısı doğrudan Go ile de kurulur:

```bash
go install github.com/KilimcininKorOglu/yada/cmd/yada@latest
```

### Kaynaktan derleme

`go.mod` Go 1.26.5 ister.

```bash
git clone https://github.com/KilimcininKorOglu/yada.git
cd yada

make build            # dist/yada (arayüz + CLI, cgo gerekir)
make build-cli        # dist/yada-cli (yalnızca CLI, statik)
make cross-cli        # beş platform için statik CLI
make cross-gui        # beş platform için arayüzlü yapı (tek makineden)
```

`make build` Fyne kullandığı için cgo ister ve her platformda ayrı derlenir. Linux'ta derlemek için `libgl1-mesa-dev`, `xorg-dev`, `libwayland-dev`, `wayland-protocols`, `libxkbcommon-dev` ve `libdecor-0-dev` paketleri gerekir; glfw Wayland arka ucunu koşulsuz derlediği için X11 paketleri tek başına yetmez.

`make build-cli`, `nogui` build tag'i ile arayüzü çıkarır. Geriye `CGO_ENABLED=0` ile derlenen statik bir binary kalır.

`make cross-gui` beş platformun arayüzlü yapısını tek bir macOS makinesinden üretir. Hedef başına bir C toolchain ve çalışan bir Docker daemon ister:

```bash
brew install FiloSottile/musl-cross/musl-cross mingw-w64
```

Linux hedefleri çapraz derlenmez, konteyner içinde derlenir: glfw hedefin X11, Wayland ve OpenGL başlıklarına karşı derleniyor ve hiçbir çapraz toolchain bunları getirmiyor. Apple Silicon üzerinde `linux/amd64` emülasyonla çalışır, yavaştır. CI'nın bu hedefe ihtiyacı yok, her platformu kendi runner'ında derliyor.

### Sürüm

Sürüm numarası git etiketinden gelir. `Makefile` onu `git describe` ile bulup ldflags ile binary'ye gömer, bu yüzden hiçbir dosyada sabit sürüm yazmaz.

```bash
yada --version
```

Etiketsiz bir ağaçtan derlenen binary sürüm yerine commit hash'i gösterir. Sürümler arasındaki değişiklikler `CHANGELOG.md` dosyasındadır.

## Ön koşullar

**Uygulamayı çalıştıran makinede:** `PATH` üzerinde bir `ssh` istemcisi. Arayüzdeki sunucu ekleme formu ayrıca aynı pakette gelen `ssh-keygen` ve `ssh-keyscan` araçlarını kullanır. Windows 10 sürüm 1809 ve sonrası OpenSSH istemcisiyle gelir; kapalıysa Ayarlar > Uygulamalar > İsteğe bağlı özellikler bölümünden eklenir.

Sistem `ssh` binary'si kullanılır, dolayısıyla `~/.ssh/config` dosyanız, ssh-agent ve `ProxyJump` gibi ayarlarınız olduğu gibi geçerlidir.

**Hedef sunucularda:** anahtar tabanlı SSH erişimi ve ayardaki `user` için parolasız `sudo`. Bağlantı testi `BatchMode=yes` ile yapılır, bu yüzden parola isteyen sunucu beklemeye girmez, ulaşılamaz olarak raporlanır.

`sudo` şu üç komut için gerekir: `unbound-checkconf`, `unbound-control` ve `systemctl`. Kayıt dosyasını okumak ve yazmak da aynı yetkiyi ister.

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

Form ad, adres, kullanıcı, port ve private key ister. Ad ile port zorunlu değildir; port boş bırakılırsa `ssh` kendi çözer. Karşılığında dört şey yazılır:

1. Private key `~/.ssh/yada_<ad>` dosyasına, `0600` izinle
2. Sunucunun host key'i `~/.ssh/known_hosts` dosyasına
3. `~/.ssh/config` içine bir `Host` bloğu (`HostName`, `User`, `Port`, `IdentityFile`, `IdentitiesOnly`)
4. Sunucu `yada.conf` içindeki `servers` listesine

`records_file`, `main_config` ve `sudo` formda yoktur, `defaults` bölümünden gelir. Bunları sunucu bazında değiştirmek isterseniz Ayarlar sekmesini kullanın.

Host key yazılmadan önce parmak izi gösterilir ve onayınız istenir. Sunucunun yöneticisinden aldığınız parmak iziyle karşılaştırın. `known_hosts` dosyasında o adres için başka bir host key kayıtlıysa hiçbir şey yazılmaz: sunucu yeniden kurulduysa ilgili satırı elle silin, kurulmadıysa bağlantı beklediğiniz makineye gitmiyordur.

Public key'in sunucudaki `authorized_keys` dosyasında zaten olması gerekir. Uygulama parola ile bağlanmaz, dolayısıyla anahtarı sunucuya kuramaz.

Sunucular aynı anahtarı paylaşabilir. Yapıştırdığınız anahtar diskte zaten varsa ikinci bir kopya yazılmaz, mevcut dosya gösterilir.

`~/.ssh/config` ve `known_hosts` yazılmadan önce `.bak` kopyası alınır. Uygulama yalnızca kendi işaretleri (`# >>> yada <adres>`) arasındaki bölümü değiştirir; elle yazdığınız bloklara dokunmaz. Aynı adres için elle yazılmış bir `Host` bloğu varsa yazma yapılmaz ve durum bildirilir.

Windows'ta OpenSSH anahtar izinlerini ACL üzerinden denetler, `0600` orada aynı anlama gelmez. Uygulama anahtarı yazdıktan sonra çalıştırmanız gereken `icacls` komutunu gösterir.

Ayar dosyasını elle yazmayı tercih ederseniz sonraki bölüme bakın.

## Ayar dosyası

`yada.conf` iki konumda aranır, ilk bulunan kullanılır:

1. Çalışan uygulamanın bulunduğu dizin
2. Kullanıcı ana dizini

`--config <yol>` her ikisini de geçersiz kılar. Örnek dosyayı oluşturmak için:

```bash
yada config init          # yada.conf üretir
yada config show          # etkin ayarı ve hangi dosyadan okunduğunu yazar
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

Bölümler: `servers` (zorunlu), `defaults`, `ssh`, `behaviour`, `log`. Sunucuda verilmeyen her alan `defaults` bölümünden tamamlanır. Tanınmayan bir anahtar sessizce yok sayılmaz, hata verir.

`records_file`, `unbound.conf` içinden `include:` ile çağrılan dosyadır. `main_config` ise `unbound-checkconf` ile doğrulanan ana dosyadır. Kayıt dosyası tek başına doğrulanamaz, çünkü `include` onu `server:` bloğunun içine gömer ve fragment kendi başına geçerli bir config değildir.

`behaviour.reload_strategy` yenileme kademesini sabitler: `auto` (varsayılan, en hafiften başlar), `local_data`, `control`, `signal`, `restart`.

`log.file` yalnızca masaüstü arayüzünü ilgilendirir; Günlük panelindeki satırlar zaman damgasıyla oraya da yazılır. Komut satırı her zaman stdout'a yazar. `log.level` doğrulanır ama henüz uygulanmaz.

Ayar dosyası binary'nin yanına konacaksa içindeki `ssh.options` dosya yolları ve `log.file` mutlak olmalıdır. Bu yollar ayar dosyasına göre değil çalışma dizinine göre çözülür.

## Masaüstü arayüzü

Altı sekme vardır:

| Sekme       | Ne yapar                                                                                                                                              |
|-------------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| Sunucular   | Tanımlı sunucuları listeler; test edildiğinde bağlantı, servis, config ve kullanılabilir yenileme kademesini gösterir. Sunucu ekleme formu buradadır. |
| Kayıtlar    | Bütün sunuculardaki kayıtları birleştirip listeler, ekleme, düzenleme ve silme yapar. Sunucuların değeri konusunda anlaşamadığı kayıtlar işaretlenir. |
| Fark        | Sunucuları karşılaştırır, eksik ve çakışan kayıtları listeler, seçilen kaynağı referans alarak eşitler.                                               |
| Toplu İşlem | CSV içe ve dışa aktarma. İçe aktarmadan önce uygulanacak ve atlanacak satırları gösterir.                                                             |
| Ayarlar     | Ayar dosyasını yerinde düzenler, kaydetmeden önce doğrular ve yükler.                                                                                 |
| Günlük      | Yapılan her işin kaydı. `log.file` tanımlıysa aynı satırlar dosyaya da yazılır.                                                                       |

Sunucu listesi ve Fark sekmesindeki kaynak seçimi ayar dosyasından doldurulur, ağa çıkmadan görünür. Sunucular sekmesinde durum sütunları test edilene kadar `denenmedi` yazar.

Uzun süren her işlem iptal edilebilir; ilerleme penceresindeki İptal düğmesi işi bağlamıyla birlikte durdurur.

## Komut satırı

Argümansız çalıştırılınca masaüstü arayüzü açılır:

```bash
yada                          # arayüz
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
yada update mail.google.com --type A --value 10.20.30.40 --ttl 300

yada delete eski.google.com
yada delete eski.google.com --type A
yada delete eski.google.com --type A --value 10.10.10.10

yada import kayitlar.csv
yada import kayitlar.csv --replace   # dosyada olmayan kayıtları da siler
yada export kayitlar.csv
yada export --type A                 # stdout'a yazar

yada diff                          # sunucular arası fark
yada sync --from ns1               # ns1'i referans alarak eşitle
yada sync --from ns1 --prune       # fazladan kayıtları da sil

yada reload                        # yalnızca yenile
yada config init                   # örnek ayar dosyası yaz
yada config show                   # etkin ayarı göster
```

### Genel bayraklar

| Bayrak           | Ne yapar                                                                     |
|------------------|------------------------------------------------------------------------------|
| `--config <yol>` | Ayar dosyasını açıkça belirtir, arama sırasını atlar.                        |
| `--server <ad>`  | Yalnızca bu sunucuda çalışır. Tekrarlanabilir; ada veya adrese göre eşleşir. |
| `--json`         | `check` ve `list` çıktısını JSON verir.                                      |
| `--dry-run`      | Hiçbir şey yazmaz, yalnızca oluşacak farkı gösterir.                         |
| `--no-reload`    | Yazar ama servisi yenilemez. Devreye almak için sonra `yada reload`.         |
| `--yes`          | Onay sorularını sormadan geçer. Etkileşimsiz çalıştırmada gereklidir.        |

### Ad zaten kullanımdaysa

`add` yazmadan önce sunucuları okur ve girdiğiniz adın ne durumda olduğuna bakar:

| Durum                      | Davranış                                                                    |
|----------------------------|-----------------------------------------------------------------------------|
| Ad hiçbir sunucuda yok     | Kaydedilir. Aynı adresin başka bir adda kullanılıyor olması engel değildir. |
| Ad var, değer birebir aynı | Yazma yapılmaz, durum bildirilir ve 0 ile çıkılır.                          |
| Ad var, değer farklı       | Hangi sunucuda ne olduğu listelenir ve değiştirilmesi için onay istenir.    |

Onay istendiğinde `--yes` sormadan devam eder, `--dry-run` sormadan yalnızca farkı gösterir. Terminal yoksa (script veya CI) komut yazmadan durur ve `--yes` ister, böylece bir otomasyon var olan kaydı kazara ezmez.

Onay verildiğinde tek karar bütün sunuculara uygulanır: kayıt eksik olan sunucuya eklenir, farklı olan sunucuda değiştirilir, zaten doğru olan sunucuda dokunulmaz.

Masaüstü arayüzü aynı denetimi yapar. Değer farklıysa "Düzenle" ve "Vazgeç" seçenekleri çıkar; "Düzenle" güncelleme penceresini açar, böylece son hâli yazılmadan önce görürsünüz.

### Silme ve eşitleme

`delete` varsayılan olarak addaki bütün tipleri siler. `--type` ve `--value` daraltır. Kaydı kalmayan ve araç tarafından eklenmiş `transparent` zone satırları da temizlenir; bunu istemiyorsanız `--prune-zones=false` verin. `static`, `refuse` ve `redirect` zone'lara hiç dokunulmaz, çünkü bunlar kayıt olmasa da geçerli bir politikadır.

`sync` kaynak sunucudaki kayıtları diğerlerine kopyalar. Sunucuların farklı değer tuttuğu kayıtlar atlanır ve listelenir, çünkü hangisinin doğru olduğu kullanıcının kararıdır. `--prune` kaynakta olmayan kayıtları hedeflerden siler.

### CSV biçimi

Başlık satırı zorunludur, sütun sırası serbesttir:

```csv
name,type,value,ttl
mail.google.com,A,10.10.10.10,
www.google.com,A,10.30.30.30,3600
ipv6.example.com,AAAA,2001:db8::1,
```

Hatalı satırlar satır numarasıyla bildirilir ve atlanır, geçerli satırlar uygulanır. Dışa aktarılan dosya doğrudan içe aktarılabilir. Aynı dosyayı ikinci kez içe aktarmak hiçbir değişiklik üretmez.

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

Her sunucu kendi dosyasını ayrı okur ve ayrı yazar. Bir sunucudaki başarısızlık diğerlerini durdurmaz, sonuç sunucu bazında raporlanır.

Kayıt içeriği hiçbir zaman uzak komut satırına gömülmez; stdin üzerinden gider, böylece kayıtlardaki tırnak ve noktalı virgül karakterleri uzak kabuk tarafından yorumlanmaz.

Dosyada yönetilmeyen satırlar (yorumlar, boş satırlar, tanınmayan direktifler) ham metin olarak saklanır ve aynen geri yazılır. Elle eklenmiş yorumlarınız silme veya güncelleme sırasında kaybolmaz.

Yalnızca son yazmanın yedeği tutulur. Zaman damgalı yedekler kimsenin temizlemediği bir sunucuda sınırsız büyürdü.

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
make fmt
make tidy
```

Testler `-count=1` ile çalışır, cache'ten sonuç dönmez. Uzak işlemler, `PATH` başına konan sahte bir `ssh` betiği ile sınanır; betik uzak komutu yerel kabukta çalıştırdığı için kabuk alıntılama ve dosya yazma gerçekten test edilir.

Bunun üstünde, SSH erişimli üç gerçek Unbound sunucusundan oluşan bir Docker ortamı vardır:

```bash
make docker-up       # üç sunucuyu ayağa kaldır
make docker-test     # ayağa kaldır ve uçtan uca senaryoyu çalıştır
make docker-logs
make docker-down
```

Doğrulamalar aracın çıktısına değil, resolver'ın verdiği yanıta bakar. Üçüncü sunucuda `remote-control` kapalıdır, böylece yenileme kademelerinin alta düşmesi de sınanır. Ayrıntılar için `docker/README.md`.

`internal/sshsetup` paketinin entegrasyon testleri bu ortama karşı gerçekten bağlanır; konteynerler kapalıyken kendilerini atlarlar:

```bash
make docker-up && go test -count=1 ./internal/sshsetup/
```

### Yapı

```
cmd/yada/              giriş noktası, arayüz ve CLI arasında seçim yapar
internal/config/       ayar yükleme, doğrulama ve düzenleme
internal/transport/    sistem ssh istemcisini çalıştırır
internal/sshsetup/     yerel anahtar, known_hosts ve ssh config yazma
internal/records/      kayıt modeli, ayrıştırma, serileştirme
internal/unbound/      okuma, yazma, doğrulama, yenileme
internal/diff/         sunucular arası karşılaştırma
internal/bulk/         CSV
internal/ui/           Fyne ekranları
```

İş mantığının tamamı `internal/` altındadır. CLI ve GUI aynı paketleri çağırır, davranışı tekrarlamaz.

`internal/ui` paketi `nogui` build tag'i ile tamamen dışarıda kalır, bu yüzden statik CLI yapısı OpenGL başlıkları olmayan bir makinede de derlenir. Bu da her değişikliğin iki varyantta da derlenip sınanmasını gerektirir.

Kullanıcıya görünen metinler Türkçe, kod yorumları İngilizcedir.

### Sürekli entegrasyon

`.github/workflows/ci.yml` her push ve pull request'te çalışır: biçim, `go mod tidy` denetimi, `go vet`, testler (`-race` ile birlikte), her iki build varyantı için lint, `govulncheck`, beş platforma çapraz derleme, üç işletim sisteminde arayüzlü derleme ve Docker ortamında uçtan uca senaryo.

`.github/workflows/release.yml` `v*` biçiminde bir etiket atıldığında binary'leri üretir, `SHA256SUMS` hesaplar ve GitHub release'i oluşturur. Release notu `CHANGELOG.md` dosyasındaki o sürümün bölümüdür.

## unbound.ps1

Depodaki PowerShell scripti bu uygulamanın öncülüdür ve çalışır durumda tutulmaktadır. Yalnızca A kaydı ekler; listeleme, güncelleme ve silme yapmaz. `Read-Host` ile soru sorduğu ve `Clear-Host` çağırdığı için etkileşimsiz çalıştırılamaz, pipe ile beslenemez.

```powershell
.\unbound.ps1
.\unbound.ps1 -UnboundServers @("10.0.0.1","10.0.0.2") -Username admin
```

Dosya BOM'lu UTF-8 olarak saklanır. Windows PowerShell 5.1 BOM'suz dosyayı ANSI okuyup Türkçe metinleri bozar.
