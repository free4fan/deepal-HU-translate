# keys/ — ключ подписи RRO-оверлеев

Это **стандартный публичный AOSP «platform» ключ** — тот самый из
`platform/build/target/product/security/` в открытых исходниках Android.
Никакого секретного в нём нет: сертификат `CN=Android, O=Android`,
SHA256 `C8:A2:E9:BC:CF:59:7C:2F:B6:DC:66:BE:E2:93:FC:13:F2:FC:47:EC:77:BC:6B:2B:0D:52:C1:1F:51:19:2A:B8`.

## Файлы

- `platform.jks` — keystore для `apksigner` (alias `androiddebugkey`, storepass `android`) — **его читают `create_rro_min.py` / `create_rro_static.py`**
- `platform.pk8` — исходный приватный ключ (PEM, base64)
- `platform.x509.pem` — сертификат
- `platform.p12` — та же пара в PKCS#12 (pass `android`)
- `pk.pem` — тот же приватный ключ в другой PEM-обёртке

## Почему в public-репозитории

Deepal собирает ГУ на стандартном AOSP platform-ключе, поэтому:

1. `platform.pk8` + `platform.x509.pem` побайтово совпадают с
   `https://github.com/aosp-mirror/platform_build/blob/main/target/product/security/platform.pk8`
   (SHAs сверены: `sha256sum` идентичны);
2. сертификат в `META-INF/CERT.RSA` заводских APK (`original_apks/*.apk`) совпадает
   с этим ключом (SHA256 `C8:A2:E9:BC:CF:59…B8`) — проверено `keytool -printcert`.

То есть ключ общедоступен в трёх независимых публичных местах: AOSP,
образы прошивки, сами заводские APK. Форкер может пересобрать оверлеи
без доступа к нашим машинам — репозиторий самодостаточен.

## Как пересобрать jks из pk8/x509 (проверка паритета)

```bash
openssl x509 -in platform.x509.pem -noout -fingerprint -sha256
# C8:A2:E9:BC:CF:59:...:B8  — сверить с apksigner verify --print-certs
```
