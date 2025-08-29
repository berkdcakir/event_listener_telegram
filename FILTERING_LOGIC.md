# Event Filtreleme Mantığı

## Genel Bakış

Bu sistem, blockchain eventlerini önem derecesine göre iki farklı Telegram grubuna yönlendirir:

- **Grup 1 (TELEGRAM_CHAT_ID_1)**: Normal/önemsiz eventler
- **Grup 2 (TELEGRAM_CHAT_ID_2)**: Önemli eventler

## Önemli Eventler (Grup 2'ye Gider)

### 1. ModuleInstalled Eventleri
- **Kriter**: Event adında "ModuleInstalled" geçen tüm eventler
- **Emoji**: 🔴
- **Açıklama**: Modül kurulum eventleri her zaman önemli kabul edilir

### 2. Yüksek Değerli Transfer Eventleri
- **Kriter**: USD değeri 250$ veya üzeri olan Transfer eventleri
- **Emoji**: 🔴
- **Açıklama**: Büyük miktarlı transferler önemli kabul edilir
- **Eşik Değeri**: `IMPORTANT_USD_THRESHOLD` environment variable ile ayarlanabilir (varsayılan: 250)

## Normal Eventler (Grup 1'e Gider)

### 1. Düşük Değerli Transfer Eventleri
- **Kriter**: USD değeri 250$ altında olan Transfer eventleri
- **Emoji**: 🔵
- **Açıklama**: Küçük miktarlı transferler normal kabul edilir

### 2. Diğer Tüm Eventler
- **Kriter**: ModuleInstalled ve Transfer dışındaki tüm eventler
- **Emoji**: 🔵
- **Örnekler**: Approval, OwnershipTransferred, DiamondCut, vb.

## Filtreleme Mantığı

```go
func determineImportance(title, body string) bool {
    // 1) ModuleInstalled her zaman önemli
    if strings.Contains(title, "ModuleInstalled") {
        return true
    }
    
    // 2) Transfer eventleri için USD miktarını kontrol et
    if strings.Contains(title, "Transfer") {
        usd := extractUSDFromBody(body)
        threshold := 250.0 // IMPORTANT_USD_THRESHOLD ile ayarlanabilir
        return usd >= threshold
    }
    
    // 3) Diğer tüm eventler önemsiz
    return false
}
```

## Environment Variables

- `TELEGRAM_CHAT_ID_1`: Normal eventler için chat ID
- `TELEGRAM_CHAT_ID_2`: Önemli eventler için chat ID
- `IMPORTANT_USD_THRESHOLD`: Transfer eventleri için USD eşik değeri (varsayılan: 250)
- `DEBUG_MODE`: Debug loglarını aktif etmek için "true" olarak ayarlayın

## Debug Logları

Sistem, `DEBUG_MODE=true` ayarlandığında her event için detaylı log çıktısı verir:

```
🔍 Önem tespiti: '🔴 [Test] ModuleInstalled'
✅ ModuleInstalled tespit edildi - ÖNEMLİ (Grup 2)

🔍 Önem tespiti: '🔴 [Test] Transfer'
💰 Transfer USD: $300.00, Eşik: $250.00, Önemli: true
✅ Transfer önemli tespit edildi - ÖNEMLİ (Grup 2)

🔍 Önem tespiti: '🔵 [Test] Transfer'
💰 Transfer USD: $100.00, Eşik: $250.00, Önemli: false
ℹ️ Transfer normal tespit edildi - NORMAL (Grup 1)

🔍 Önem tespiti: '🔵 [Test] Approval'
ℹ️ Diğer event tespit edildi - NORMAL (Grup 1)
```

**Not**: Production ortamında `DEBUG_MODE` ayarlanmamalıdır çünkü log spam'i oluşturabilir.

## Test Fonksiyonu

`TestImportanceFiltering()` fonksiyonu ile filtreleme mantığını test edebilirsiniz:

```go
listener.TestImportanceFiltering()
```

Bu fonksiyon, farklı event türleri için beklenen sonuçları loglar.

## Özet

### Önemli Eventler (Grup 2)
- ✅ ModuleInstalled eventleri
- ✅ 250$+ Transfer eventleri

### Normal Eventler (Grup 1)
- ✅ 250$ altı Transfer eventleri
- ✅ Diğer tüm eventler (Approval, OwnershipTransferred, vb.)

### Debug ve Test
- `DEBUG_MODE=true` ile detaylı loglar
- `TestImportanceFiltering()` ile filtreleme testi
- `IMPORTANT_USD_THRESHOLD` ile eşik değeri ayarlama

Bu filtreleme sistemi sayesinde önemsiz eventlerin grup 2'ye gitmesi engellenmiş olur.
