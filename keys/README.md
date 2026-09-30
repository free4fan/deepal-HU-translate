# keys/ — ключ подписи RRO-оверлеев

Здесь лежат файлы, которые **намеренно не публикуются** в git (см. `..gitignore`):

- `platform.jks` — keystore (alias `androiddebugkey`, storepass `android`) для `apksigner`
- `platform.pk8` / `platform.x509.pem` — исходная пара (AOSP-формат)
- `platform.p12` / `pk.pem` — те же ключи в других контейнерах

## Что это за ключ

Это **стандартный AOSP "platform" ключ** (`build/target/product/security/` в
исходниках Android) — сертификат `CN=Android, O=Android`, SHA256
`C8:A2:E9:BC:CF:59:7C:2F:B6:DC:66:BE:E2:93:FC:13:F2:FC:47:EC:77:BC:6B:2B:0D:52:C1:1F:51:19:2A:B8`.

Проверено: сертификат в `META-INF/CERT.RSA` заводских APK (`original_apks/*.apk`)
совпадает с этим ключем один в один — Deepal собирает ГУ на стандартном
AOSP platform-ключе, поэтому подпись наших RRO им совместима (PMS принимает
оверлей как собственный платформенный).

## Как получить копию

1. АOSP: `platform.pk8` + `platform.x509.pem` — в публичных исходных
   `build/target/product/security/` (например, в образе прошивки / любой
   `android/build/`).
2. Из заводского APK: `apksigner verify --print-certs original_apks/<App>.apk`
   даёт fingerprint для сверки (приватную часть ключа из APK не извлечь —
   только сертификат).
3. Из образа прошивки ГУ: `/apex/...system/etc/security/...` (публичный образ).

## Зачем

`create_rro_min.py` / `create_rro_static.py` подписывают собранные RRO-APK
`apksigner --ks keys/platform.jks`. Без ключа собрать подписанную подпись
нельзя (PMS отклонит оверлей с чужой подписью: «signed with different
certificates, and the overlay lacks <overlay android:targetName>» — см.
CHANGELOG [2026-09-22b] про 9 AOSP-target).
