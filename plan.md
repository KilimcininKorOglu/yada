# Unbound DNS Yöneticisi: Go Uygulaması Planı

Mevcut `unbound.ps1` scriptini Go ile yazılmış, Windows, Linux ve macOS üzerinde çalışan bir CLI ve GUI uygulamasına dönüştürme planı.

## 1. Kararlar

Bu kararlar plan yazılmadan önce onaylandı.

| Konu | Karar |
|---|---|
| GUI | Fyne |
| SSH | Sistem `ssh` binary'si, `os/exec` ile |
| Ayar formatı | YAML, dosya adı `unbound-dns.conf` |
| Ayar önceliği | Önce uygulamanın yanı, sonra kullanıcı dizini |
| Kapsam | Listeleme, silme, A dışı kayıt tipleri, CSV toplu işlem, sunucular arası fark |
| Platformlar | Windows, Linux, macOS |

## 2. Binary yapısı

**İki ayrı binary üretilecek.** Gerekçe: Fyne CGO ve OpenGL gerektirir. Tek binary yapılırsa CLI de CGO'ya bağlanır, `CGO_ENABLED=0` ile statik derlenemez ve cross-compile için her hedefte C toolchain gerekir.

| Binary | Bağımlılık | Kullanım |
|---|---|---|
| `unbound-dns` | Saf Go, `CGO_ENABLED=0` | CLI. Her platforma tek komutla cross-compile edilir, sunuculara kopyalanabilir. |
| `unbound-dns-gui` | Fyne, CGO | Masaüstü arayüzü. Platform başına ayrı derlenir. |

Ortak mantığın tamamı `internal/` altında toplanır. GUI, CLI'ın kullandığı paketleri çağırır, iş mantığını tekrarlamaz.

Tek binary tercih edilirse `cmd/unbound-dns/gui.go` dosyasına `//go:build gui` etiketi konarak `make build-single` hedefiyle üretilebilir. Plan iki binary üzerine kuruludur.

## 3. Dizin yapısı

```
unbound-dns/
├── cmd/
│   ├── unbound-dns/            # CLI giriş noktası (cobra)
│   └── unbound-dns-gui/        # Fyne giriş noktası
├── internal/
│   ├── config/                 # unbound-dns.conf yükleme, arama sırası, doğrulama
│   ├── transport/              # sistem ssh sarmalayıcı, komut çalıştırma
│   ├── records/                # RR modeli, ayrıştırma, serileştirme, doğrulama
│   ├── unbound/                # checkconf, üç kademeli yenileme, dosya yazma
│   ├── diff/                   # sunucular arası karşılaştırma ve eşitleme
│   ├── bulk/                   # CSV içe/dışa aktarma
│   └── ui/                     # Fyne ekranları ve bileşenleri
├── testdata/                   # örnek local_records.conf, CSV örnekleri
├── Makefile
├── go.mod
├── unbound-dns.conf.example
└── README.md
```

## 4. Ayar dosyası

### 4.1 Arama sırası

1. Çalışan binary'nin bulunduğu dizin: `<exe_dir>/unbound-dns.conf`
2. Kullanıcı ana dizini: `<home>/unbound-dns.conf`

İlk bulunan kullanılır, dosyalar birleştirilmez. `--config <yol>` bayrağı her ikisini de geçersiz kılar.

Binary dizini `os.Executable()` ile bulunur ve `filepath.EvalSymlinks` ile çözülür, aksi halde symlink üzerinden çalıştırıldığında yanlış dizine bakılır.

Hiçbir dosya bulunamazsa uygulama hata verip durur. CLI'da `unbound-dns config init` komutu, GUI'da ilk açılış sihirbazı örnek dosyayı oluşturur.

### 4.2 Şema

```yaml
# Bağlanılacak Unbound sunucuları
servers:
  - name: ns1                              # görünen ad, zorunlu değil
    host: 192.0.2.4
    user: user01
    port: 22                               # varsayılan 22
    records_file: /etc/unbound/local_records.conf
    main_config: /etc/unbound/unbound.conf
  - name: ns2
    host: 192.0.2.5
    user: user01

# Sunucu bazında verilmeyen alanlar buradan alınır
defaults:
  user: user01
  port: 22
  records_file: /etc/unbound/local_records.conf
  main_config: /etc/unbound/unbound.conf
  sudo: true                               # uzak komutlar sudo ile çalışsın mı

ssh:
  binary: ssh                              # PATH'te başka bir isim kullanılıyorsa
  connect_timeout: 10s
  options:                                 # ssh -o olarak geçirilir
    - BatchMode=yes
  config_file: ""                          # boş ise ssh kendi ~/.ssh/config dosyasını kullanır

behaviour:
  parallel: true                           # sunuculara eşzamanlı bağlan
  max_parallel: 4
  backup_before_write: true                # yazmadan önce .bak dosyası oluştur
  reload_strategy: auto                    # auto | control | signal | restart
  confirm_destructive: true                # CLI'da silme ve eşitleme için onay iste

log:
  level: info                              # debug | info | warn | error
  file: ""                                 # boş ise sadece ekrana yazar
```

### 4.3 Doğrulama

Yükleme sırasında kontrol edilenler: en az bir sunucu tanımlı olmalı, her sunucunun `host` alanı dolu olmalı, `user` sunucu veya `defaults` içinde bulunmalı, `records_file` ve `main_config` mutlak yol olmalı, `reload_strategy` bilinen bir değer olmalı. Eksik alan bulunursa hangi sunucuda hangi alanın eksik olduğu tek tek bildirilir.

## 5. SSH taşıma katmanı

`internal/transport` paketi sistem `ssh` binary'sini sarmalar.

```go
type Runner interface {
    Run(ctx context.Context, srv config.Server, cmd string) (Result, error)
    RunWithStdin(ctx context.Context, srv config.Server, cmd string, stdin io.Reader) (Result, error)
}

type Result struct {
    Stdout   string
    Stderr   string
    ExitCode int
}
```

Kurallar:

- Uzak komut `ssh` argümanlarının **sonuncusu** olarak geçirilir.
- Çıkış kodu her çağrıda okunur ve döndürülür. Sıfırdan farklı kod hata sayılır ve `stderr` içeriğiyle birlikte yüzeye çıkar, sessizce yutulmaz.
- `ssh` binary'si `PATH`'te bulunamazsa kullanıcıya platforma özel kurulum yönlendirmesi verilir. Windows'ta OpenSSH istemcisi 1809 ve sonrası ile birlikte gelir, kapalıysa isteğe bağlı özelliklerden açılır.
- Bağlantı testi `ssh -o BatchMode=yes -o ConnectTimeout=<n> <user>@<host> "echo ok"` ile yapılır. `BatchMode` sayesinde parola isteyen sunucu beklemeye girmez, ulaşılamaz sayılır.
- `context.Context` iptal edildiğinde alt süreç öldürülür, GUI'da "İptal" düğmesi bunu kullanır.

### 5.1 Enjeksiyon önlemi

Kullanıcıdan gelen hiçbir değer uzak komut satırına gömülmez. Dosya içeriği `RunWithStdin` ile `sudo tee <dosya> > /dev/null` komutuna stdin üzerinden aktarılır. Böylece kayıt adı veya değeri içindeki tırnak, noktalı virgül ve backtick karakterleri kabuk tarafından yorumlanmaz.

Yalnızca ayar dosyasından gelen dosya yolları komut satırına girer. Bunlar da yükleme sırasında mutlak yol ve kabuk metakarakteri içermeme koşuluyla doğrulanır.

## 6. Kayıt modeli

### 6.1 Veri yapısı

```go
type Record struct {
    Name  string      // mail.google.com.  (sonda nokta normalize edilir)
    TTL   *uint32     // nil ise dosyada yazılmaz
    Class string      // varsayılan IN
    Type  RecordType  // A, AAAA, CNAME, TXT, MX, PTR
    Value string      // tipe göre anlamı değişir
}

type Zone struct {
    Name string       // google.com.
    Type string       // transparent, static, redirect ...
}
```

### 6.2 Ayrıştırma

`local_records.conf` satır satır okunur. `local-data:` ve `local-zone:` satırları yakalanır, tırnak içindeki gövde ayrıştırılır. Tanınmayan satırlar, yorumlar ve boş satırlar **olduğu gibi saklanır**; dosya yeniden yazılırken korunur. Kullanıcının elle eklediği yorumların silinmemesi için bu şart.

Dosya modeli:

```go
type File struct {
    Lines []Line   // sıra korunur
}

type Line struct {
    Raw     string   // özgün metin
    Kind    LineKind // Comment | Blank | Zone | Data | Unknown
    Record  *Record  // Kind == Data ise dolu
    Zone    *Zone    // Kind == Zone ise dolu
}
```

### 6.3 Serileştirme

Mevcut scriptin ürettiği biçim korunur: dokuz boşluk girinti, değer çift tırnak içinde.

```
         local-zone: "google.com." transparent
         local-data: "mail.google.com. IN A 10.10.10.10"
```

Değiştirilmemiş satırlar `Raw` alanından aynen yazılır, yalnızca eklenen ve düzenlenen satırlar yeniden üretilir. Böylece mevcut dosyada gereksiz fark oluşmaz.

### 6.4 Tip bazlı doğrulama

| Tip | Değer | Doğrulama |
|---|---|---|
| A | IPv4 | `net.ParseIP`, `To4()` sonucu nil olmamalı |
| AAAA | IPv6 | `net.ParseIP`, `To4()` nil olmalı |
| CNAME | Hedef ad | Alan adı biçimi, kendine döngü kontrolü |
| TXT | Serbest metin | 255 baytlık parçalara bölme, tırnak kaçışı |
| MX | `<öncelik> <hedef>` | Öncelik 0-65535, hedef alan adı |
| PTR | Hedef ad | Ad `in-addr.arpa.` veya `ip6.arpa.` ile bitmeli |

Alan adı doğrulaması: etiket başına en fazla 63 karakter, toplam 253 karakter, izin verilen karakter kümesi harf, rakam, tire; etiket tire ile başlayamaz veya bitemez. Uluslararası alan adları `golang.org/x/net/idna` ile punycode'a çevrilir.

PTR için kolaylık: kullanıcı `10.10.10.10` girerse uygulama bunu `10.10.10.10.in-addr.arpa.` biçimine çevirmeyi önerir.

### 6.5 Zone yönetimi

Bir kayıt eklenirken üst zone yoksa `local-zone: "<zone>." transparent` satırı eklenir. Zone adı, kaydın ilk etiketi atılarak bulunur. Zone zaten varsa dokunulmaz ve tipi değiştirilmez.

## 7. Yazma akışı

Kayıt ekleme, silme ve güncelleme aynı akışı kullanır. Bu akış sunucu başına çalışır.

1. **Oku.** `sudo cat <records_file>` ile mevcut içerik alınır ve ayrıştırılır.
2. **Değiştir.** İstenen ekleme, silme veya güncelleme bellekte uygulanır.
3. **Yedekle.** `backup_before_write` açıksa `sudo cp <records_file> <records_file>.bak` çalıştırılır. Tek bir `.bak` tutulur, her yazımda üzerine yazılır; zaman damgalı yedek biriktirmek disk doldurur.
4. **Yaz.** Yeni içerik `sudo tee <records_file> > /dev/null` komutuna stdin üzerinden verilir.
5. **Doğrula.** `sudo unbound-checkconf <main_config>` çalıştırılır.
6. **Geri al.** Doğrulama başarısızsa `sudo cp <records_file>.bak <records_file>` ile eski hal geri yüklenir, `unbound-checkconf` çıktısı kullanıcıya gösterilir ve o sunucu için işlem başarısız sayılır. Servise dokunulmaz.
7. **Yenile.** Doğrulama başarılıysa bölüm 8'deki yenileme uygulanır.

Doğrulama neden ana config üzerinden yapılır: `include:` direktifi `local_records.conf` içeriğini `server:` clause'unun içine gömer. Fragment tek başına geçerli bir config değildir, doğrudan `unbound-checkconf` ile kontrol edilemez.

Bir sunucudaki başarısızlık diğerlerini durdurmaz. Her sunucunun sonucu ayrı raporlanır.

## 8. Yenileme stratejisi

Üç kademe, en hafiften en ağıra. Her kademe başarısız olursa bir sonrakine geçilir ve geçiş nedeni kullanıcıya yazılır.

| Sıra | Komut | Kesinti | Cache | Ön koşul |
|---|---|---|---|---|
| 1 | `unbound-control reload_keep_cache` | Yok | Korunur | `remote-control: control-enable: yes` ve `unbound-control-setup` sertifikaları |
| 2 | `systemctl reload unbound` (SIGHUP) | Yok | Silinir | Unit dosyasında `ExecReload` tanımlı olmalı |
| 3 | `systemctl restart unbound` | Var | Silinir | Yok |

İkinci kademeden sonra `systemctl is-active unbound` ile durum doğrulanır. Daemon config'i yeniden okurken reddederse süreç ölür, ancak `systemctl reload` yine de sıfır döner; bu kontrol olmadan ölü servis başarılı sayılır.

Üçüncü kademeden sonra `is-active` iki saniye aralıkla en fazla on beş kez yoklanır.

`reload_strategy` ayarı `control`, `signal` veya `restart` olarak sabitlenirse yalnızca o kademe denenir, başarısız olursa alt kademeye düşülmez. Varsayılan `auto` üç kademeyi sırayla dener.

## 9. CLI

`cobra` kullanılır. Tüm komutlar `--config`, `--server` (yalnızca belirtilen sunucuya uygula, tekrarlanabilir), `--json` (makine okunur çıktı) ve `--dry-run` bayraklarını destekler.

```
unbound-dns config init                       # örnek ayar dosyası oluştur
unbound-dns config show                       # etkin ayarı ve hangi dosyadan okunduğunu göster
unbound-dns check                             # bağlantı + unbound-checkconf, hiçbir şey değiştirmez

unbound-dns list [--type A] [--filter google] # kayıtları tablo veya JSON olarak listele
unbound-dns add <ad> <tip> <değer> [--ttl 3600]
unbound-dns delete <ad> [--type A] [--value 10.10.10.10]
unbound-dns update <ad> --type A --value <yeni-ip>

unbound-dns import <dosya.csv> [--replace]    # toplu ekleme
unbound-dns export <dosya.csv> [--type A]     # toplu dışa aktarma

unbound-dns diff                              # sunucular arası fark
unbound-dns sync --from ns1 [--dry-run]       # kaynak sunucuyu referans alarak eşitle

unbound-dns reload                            # sadece yenileme, kayıt değiştirmez
```

`--dry-run` yazma yapmaz, yalnızca üretilecek dosya farkını gösterir. `confirm_destructive` açıkken `delete` ve `sync` onay ister; `--yes` bayrağı onayı atlar ve otomasyonu mümkün kılar.

Çıkış kodları: `0` başarı, `1` genel hata, `2` ayar hatası, `3` bağlantı hatası, `4` config doğrulama hatası, `5` kısmi başarı (bazı sunucular başarısız).

## 10. GUI

Fyne ile tek pencere, sol tarafta gezinme, sağ tarafta içerik.

**Ekranlar:**

1. **Sunucular.** Her sunucunun bağlantı durumu, Unbound sürümü, kayıt sayısı, `unbound-control` kullanılabilirliği. "Tümünü test et" düğmesi.
2. **Kayıtlar.** Filtrelenebilir ve sıralanabilir tablo: Ad, Tip, Değer, TTL, hangi sunucularda var. Çoklu seçim, toplu silme. Sağ üstte arama kutusu.
3. **Kayıt ekle/düzenle.** Tip seçimine göre değişen form. Değer alanı tipe uygun doğrulanır, hata alanın altında anında gösterilir. Hedef sunucular onay kutularıyla seçilir.
4. **Fark.** İki sunucu seçilir, yalnızca birinde bulunan kayıtlar renkli işaretlenir. Seçilen kayıtlar için "Eksik olana kopyala" düğmesi.
5. **Toplu işlem.** CSV dosyası seç, önizleme tablosunda geçerli ve hatalı satırları ayrı göster, yalnızca geçerli olanları uygula.
6. **Ayarlar.** Ayar dosyasının yolu ve hangi konumdan yüklendiği, sunucu ekleme/çıkarma formu, yenileme stratejisi seçimi.
7. **Günlük.** Çalıştırılan uzak komutlar ve çıktıları. Sorun bildirirken kopyalanabilir olması için "Panoya kopyala" düğmesi.

**Davranış kuralları:**

- Uzun süren işlemler goroutine'de çalışır, arayüz donmaz. İlerleme çubuğu ve "İptal" düğmesi `context` iptali ile bağlanır.
- Arayüzü güncelleyen her çağrı Fyne'ın ana döngüsünde yapılır; goroutine içinden doğrudan bileşen güncellenmez.
- Yıkıcı işlemler (silme, eşitleme) onay diyaloğu ister ve etkilenecek kayıt sayısını gösterir.
- Hata diyalogları uzak komutun `stderr` çıktısını gizlemez, katlanabilir bir alanda gösterir.

## 11. CSV biçimi

Başlık satırı zorunlu. Sütun sırası serbest, isimlerle eşleştirilir.

```csv
name,type,value,ttl
mail.google.com,A,10.10.10.10,
www.google.com,A,10.30.30.30,3600
ipv6.example.com,AAAA,2001:db8::1,
alias.example.com,CNAME,www.google.com,
```

İçe aktarmada her satır tek tek doğrulanır. Hatalı satırlar satır numarası ve nedeniyle raporlanır, geçerli satırlar uygulanır; tek hatalı satır yüzünden tüm dosya reddedilmez. `--replace` verilirse dosyadaki kayıt kümesi mevcut kayıtların yerine geçer, verilmezse üzerine eklenir.

Dışa aktarma aynı biçimi üretir, böylece dışa aktarılan dosya doğrudan içe aktarılabilir.

## 12. Fark ve eşitleme

Karşılaştırma normalize edilmiş kayıt üzerinden yapılır: ad küçük harfe çevrilir ve sonuna nokta eklenir, tip büyük harfe çevrilir, TTL karşılaştırmaya dahil edilmez (isteğe bağlı `--with-ttl` ile dahil edilir).

Üç durum raporlanır: yalnızca A sunucusunda olan, yalnızca B sunucusunda olan, iki tarafta da olup değeri farklı olan. Üçüncü durum çakışmadır ve otomatik eşitlenmez, kullanıcıdan hangi tarafın doğru olduğu sorulur.

`sync --from ns1` kaynak sunucuyu referans alır ve hedeflerde eksik olanları ekler. Varsayılan olarak hedefte fazladan bulunan kayıtları silmez; silme `--prune` ile açıkça istenir.

## 13. Test stratejisi

- **Birim testleri.** Ayrıştırma ve serileştirme (round-trip: ayrıştır, geri yaz, özgün dosyayla karşılaştır), tip doğrulama, alan adı doğrulama, CSV ayrıştırma, fark hesaplama, ayar arama sırası.
- **Altın dosya testleri.** `testdata/` altındaki örnek `local_records.conf` dosyaları üzerinde ekleme ve silme sonucu beklenen çıktıyla karşılaştırılır.
- **Entegrasyon testleri.** `PATH` başına sahte bir `ssh` betiği konur. Betik uzak komutu yerel kabukta çalıştırır, böylece kabuk alıntılama ve dosya yazma gerçekten sınanır. Senaryolar: `unbound-control` yok, `systemctl reload` desteklenmiyor, `checkconf` başarısız ve geri alma, kısmi sunucu başarısızlığı.
- **Testler önbelleksiz çalışır.** Tüm test hedefleri `go test -count=1` kullanır.

Kapsam hedefi: `internal/records` ve `internal/config` için yüzde seksenin üzeri. GUI kodu için birim testi hedeflenmez, iş mantığı GUI dışında tutulduğu için gerek kalmaz.

## 14. Derleme ve dağıtım

`Makefile` hedefleri:

```
make build          # yerel platform için her iki binary
make build-cli      # CGO_ENABLED=0, saf Go
make build-gui      # Fyne, yerel platform
make cross-cli      # linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
make cross-gui      # fyne-cross ile Docker üzerinden
make test           # go test -count=1 ./...
make lint           # golangci-lint run
make vuln           # govulncheck ./...
make package        # arşivler ve macOS .app, Windows .exe kaynakları
make clean
```

CLI cross-compile için ek araç gerekmez. GUI için `fyne-cross` (Docker tabanlı) kullanılır; alternatif olarak GitHub Actions üzerinde `windows-latest`, `macos-latest` ve `ubuntu-latest` matrisi ile her platformda native derleme yapılır. Native derleme tercih edilir, çünkü macOS imzalama ve notarization yalnızca macOS üzerinde yapılabilir.

Platform notları:

- **Linux GUI:** derleme için `libgl1-mesa-dev`, `xorg-dev` paketleri gerekir.
- **Windows GUI:** `-ldflags -H=windowsgui` ile konsol penceresi gizlenir. CLI'da bu bayrak kullanılmaz.
- **macOS GUI:** `.app` paketi `fyne package` ile üretilir. İmzasız dağıtımda Gatekeeper uyarısı çıkar, dağıtım şekli sonradan kararlaştırılabilir.

Sürüm bilgisi `-ldflags "-X main.version=..."` ile gömülür, `unbound-dns version` komutuyla gösterilir.

## 15. Uygulama sırası

Her aşama kendi içinde çalışır durumda bitirilir ve ayrı ayrı commit edilir.

**Durum: tüm aşamalar tamamlandı.** Uygulama sırasında planın 4.2, 6.5 ve 8. bölümlerinde düzeltmeler yapıldı; bunlar ilgili bölümlere işlendi.

| Aşama | İçerik | Biterken doğrulanan |
|---|---|---|
| 1 | Proje iskeleti, `go.mod`, Makefile, `internal/config` | Ayar dosyası iki konumdan da yükleniyor, eksik alanlar raporlanıyor |
| 2 | `internal/transport`, `unbound-dns check` | Sahte `ssh` ile bağlantı testi ve hata kodları doğru raporlanıyor |
| 3 | `internal/records` ayrıştırma ve serileştirme, `list` | Round-trip testi geçiyor, yorumlar korunuyor |
| 4 | Yazma akışı, `add`, checkconf, geri alma | Bozuk config yazıldığında dosya eski haline dönüyor |
| 5 | Üç kademeli yenileme, `reload` | Üç senaryo (control yok, reload yok, ikisi de yok) doğru kademeye düşüyor |
| 6 | `delete`, `update` | Silinen satır dışındaki içerik bit düzeyinde korunuyor |
| 7 | A dışı kayıt tipleri | Her tip için doğrulama testleri geçiyor |
| 8 | `import`, `export` | Dışa aktarılan dosya kayıpsız içe aktarılıyor |
| 9 | `diff`, `sync` | Çakışma otomatik eşitlenmiyor, `--prune` olmadan silme yapılmıyor |
| 10 | GUI: sunucular, kayıtlar, ekleme ekranları | Uzun işlemde arayüz donmuyor |
| 11 | GUI: fark, toplu işlem, ayarlar, günlük | Yıkıcı işlemler onay istiyor |
| 12 | Paketleme, README, sürüm bilgisi | Üç platformda binary üretiliyor |

## 16. Kapsam dışı

Bu sürümde yapılmayacaklar, sonraya bırakılanlar:

- Unbound dışındaki DNS sunucuları.
- `unbound-control local_data` ile çalışma zamanı kayıt ekleme. Kalıcı olmadığı için dosya tabanlı akışla birlikte iki kaynak doğruluk sorunu yaratır.
- View ve tag tabanlı yapılandırma.
- Sunucu tarafına ajan kurulumu. Uygulama yalnızca SSH üzerinden çalışır.
- DNSSEC anahtar yönetimi.
- Kayıt geçmişi ve sürümleme. `.bak` dosyası yalnızca son yazımı saklar.

## 17. Açık noktalar

Uygulama, aşağıdaki varsayımlarla tamamlandı. Farklıysa ek iş gerekir:

1. **Parolasız `sudo` varsayıldı.** Sunucularda `$Username` için `unbound-checkconf`, `unbound-control` ve `systemctl` komutlarının parolasız çalıştığı kabul edildi. Parola isteniyorsa GUI'ya güvenli sorma akışı, CLI'ya terminal isteği eklenmelidir.
2. **Varsayılan araç yolları varsayıldı.** `unbound-checkconf`, `unbound-control` ve `systemctl` komutlarının `sudo` secure_path üzerinden bulunduğu kabul edildi. Dağıtımınızda farklıysa ayar dosyasına bir `paths` bölümü eklenmelidir.
3. **Geliştirme bu repoda yapıldı** ve `unbound.ps1` yerinde bırakıldı. Go sürümü onu işlevsel olarak kapsıyor; kaldırma kararı kullanıcıya ait.

### Sonraki adım adayları

- GUI için macOS imzalama ve notarization, Windows kod imzalama.
- GitHub Actions ile üç platformda native GUI derlemesi.
- `unbound-control local_data` ile çalışma zamanı ekleme (kalıcılık dosya üzerinden sürer).
