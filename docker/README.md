# Test ortamı

SSH erişimli üç Unbound sunucusu. Amaç, CLI'yi taklit edilmiş bir katman yerine gerçek bir daemon'a karşı çalıştırmak: gerçek `ssh`, gerçek `sudo`, gerçek `unbound-checkconf`, gerçek `unbound-control`.

Buradaki hiçbir şey dağıtılmaz. Ortam kurulup yıkılmak için vardır.

## Çalıştırma

```bash
make docker-test        # anahtar üret, ayağa kaldır, uçtan uca senaryoyu çalıştır
make docker-up          # yalnızca ayağa kaldır
make docker-down        # kaldır ve volume'ları sil
make docker-logs
```

Elle komut çalıştırmak için:

```bash
dist/unbound-dns-cli --config docker/unbound-dns.docker.conf check
dist/unbound-dns-cli --config docker/unbound-dns.docker.conf add a.example.test A 10.1.2.3
```

Ayar dosyası `--config` ile açıkça verilir. Otomatik aramaya bırakılsaydı gerçek bir `unbound-dns.conf` dosyasını gölgeleyebilirdi.

## Sunucular

| Sunucu | SSH | DNS | remote-control | Beklenen yenileme kademesi |
|---|---|---|---|---|
| ns1 | 8340 | 8341 | açık | `local_data` |
| ns2 | 8342 | 8343 | açık | `local_data` |
| ns3 | 8344 | 8345 | **kapalı** | `systemctl reload` |

ns3'te kontrol kanalı bilerek kapalıdır. Kademe düşüşünü sınayan tek şey odur; her sunucunun kusursuz yapılandırıldığı bir ortamda en hafif kademe dışında hiçbir yol çalışmaz.

Portlar 127.0.0.1'e bağlanır, dışarıya açılmaz.

## Bilinmesi gerekenler

**systemd yok.** Konteynerde `/usr/local/bin/systemctl` gerçek systemd değil, `unbound` sürecini doğrudan yöneten bir kalıptır. `is-active`, `reload`, `restart` ve `show --property=CanReload` sorularını yanıtlar. Böylece systemctl'e dayanan iki kademe gerçek kod yolunda kalır. Kalıp bilinçli olarak genel amaçlı değildir; tanımadığı bir işlemi sessizce başarılı saymak yerine hata verir.

**Unbound root olarak çalışır.** Pidfile ve kontrol soketi için ek uğraş gerektirmez. Gerçek bir kurulumda yapılmaz; tek kullanımlık bir konteynerde korunacak bir şey yoktur.

**DNSSEC doğrulaması kapalıdır.** Konteynerin kök sunuculara yolu yoktur, açık olsaydı her sorgu doğrulamada düşer ve gerçek hataları maskelerdi.

**Anahtarlar depoda değildir.** `docker/keys/` `.gitignore` içindedir. `make docker-keys` çalıştığı makinede tek kullanımlık bir ed25519 anahtarı üretir. Konteynerlerin host anahtarları da her açılışta yeniden üretilir, bu yüzden CLI ayarı `StrictHostKeyChecking=no` kullanır.

## Senaryo

`integration-test.sh` on bir adımdan geçer. Her doğrulama, aracın ekrana yazdığına değil, resolver'ın gerçekten verdiği yanıta bakar; böylece doğru dosyayı yazıp daemon'a hiç ulaşmayan bir değişiklik yine de başarısız olur.

1. `check` üç sunucuyu da görür, kademeleri doğru raporlar
2. `add` üç sunucuya yazar, kayıt anında çözülür, ns1 `local_data` kullanır, ns3 sinyale düşer
3. Elle yazılmış yorumlar ve önceden var olan kayıt korunur
4. Aynı kayıt ikinci kez eklenmez
5. `update` sonrası daemon yeni değeri verir
6. Bir addaki A kaydı silinince aynı addaki TXT kaydı ayakta kalır
7. `import` ile CSV toplu yükleme
8. `export` çıktısı geri yüklenebilir
9. `diff` farkı bulur, `sync` yayar, sonrasında sunucular eşit
10. Ana config bozulunca yazma başarısız olur ve kayıt dosyası bayt bayt eski haline döner
11. Temizlik

Altıncı adım en ince olanıdır. `unbound-control local_data_remove` bir addaki bütün tipleri birden siler, dolayısıyla dosyada duran diğer kayıtların aynı işlemde geri yazılması gerekir. Bunu yalnızca çalışan bir daemon'a sorarak doğrulamak mümkündür.
