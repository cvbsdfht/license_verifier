# license-verifier (Go / Fiber)

โมดูลตรวจ license token แบบ **offline** สำหรับฝังใน service Go (Fiber) ที่ deploy ฝั่งลูกค้า
token format ตรงกับ `license-signer` ฝั่งเรา (Ed25519) — ใช้ token เดียวกันได้เลย

**core ใช้ standard library ล้วน** (`crypto/ed25519`) มีแค่ `middleware.go` ที่ผูกกับ Fiber (v3)

## โครงสร้าง

```
license-verifier/
├── main.go                 # ตัวอย่าง wiring + //go:embed public.pem
├── public.pem              # ed25519 public key (ฝังเข้า binary)
└── core/license/           (package license)
	├── verifier.go         # core ตรวจ token (pure, ไม่ panic) — stdlib
	├── clockguard.go       # high-water mark กัน clock rollback — stdlib
	├── license.go          # License: New/Start/Stop/State — stdlib
	└── middleware.go       # Fiber middleware + StatusHandler (ผูก fiber)
```

> หมายเหตุ: โฟลเดอร์ชื่อมี hyphen ได้ แต่ชื่อ package ใน Go ห้ามมี hyphen
> จึงประกาศเป็น `package license` แล้ว import ด้วย path `license_verifier/core/license` เรียกว่า `license.New(...)`

## ใช้งาน

```go
import license "license_verifier/core/license"

lic, err := license.New(license.Config{
	PublicKeyPEM: publicKeyPEM,                  // baked ตอน build
	DeploymentId: "customer-acme-prod",          // baked — deployment_id ของลูกค้ารายนี้
	BuildTime:    buildTime,
	TokenPath:    "/etc/license/license.token",  // Secret ที่ลูกค้า mount
	ClockFile:    "/var/lib/app/.license-hwm",   // PVC (persistent + เขียนได้)
})

if err != nil {
	log.Fatal(err)
}

lic.Start()        // เริ่ม goroutine เช็คซ้ำเป็นระยะ
defer lic.Stop()   // ปิด goroutine

app.Get("/license/status", lic.StatusHandler()) // ไม่ผ่าน gate
app.Use(lic.FiberMiddleware())                  // gate route หลังจากนี้
```

### Config (ค่า default)

| Field | ความหมาย | default |
|---|---|---|
| `PublicKeyPEM` | ed25519 public key (PEM/SPKI) — **required** | - |
| `DeploymentId` | deployment_id ที่คาดหวัง — **required** | - |
| `BuildTime` | เวลา build image (เป็น floor ของ clock guard) | - |
| `TokenPath` | ไฟล์ token ที่ลูกค้า mount | - |
| `ClockFile` | ไฟล์ high-water mark | - |
| `ExpiringSoonDays` | กี่วันก่อนหมดถือว่าใกล้หมด | 30 |
| `RecheckInterval` | รอบเช็คซ้ำ + อ่าน token ใหม่ | 1 นาที |
| `OnChange` | callback เมื่อสถานะเปลี่ยน | nil |

## สถานะ

| Status | serving? |
|---|---|
| `VALID` / `EXPIRING_SOON` / `GRACE` | ใช่ |
| `NOT_YET_VALID` / `EXPIRED` / `INVALID` | ไม่ |

`INVALID` มี `Reason` เช่น `TOKEN_EMPTY`, `TOKEN_MALFORMED`, `SIGNATURE_DECODE`, `SIGNATURE_INVALID`,
`PAYLOAD_DECODE`, `PAYLOAD_UNREADABLE`, `DEPLOYMENT_MISMATCH`, `BAD_EXPIRY`, `CLOCK_TAMPERED`, `TOKEN_FILE_MISSING`

## bake ค่าตอน build (ห้ามอ่านจาก env ที่ลูกค้าแก้ได้)

- **public key**: ใช้ `//go:embed public.pem` ฝังเข้า binary ตอน compile
- **deployment_id / build_time**: ฉีดผ่าน ldflags (ตรงกับตัวแปรใน `main.go`)

```bash
export LICENSE_TOKEN_PATH="./license.token"
export LICENSE_CLOCK_FILE="./.license-hwm"

go build -ldflags "-X 'main.deploymentID=customer-acme-prod' \
	-X 'main.buildTimeRaw=2026-06-09T00:00:00Z'"

go run -ldflags "-X 'main.deploymentID=bookzaa001' \
	-X 'main.buildTimeRaw=2026-06-09T00:00:00Z'" .
```

## หลักการ

- บังคับที่ backend ผ่าน middleware — หน้าบ้านแค่โชว์สถานะ
- หมด license -> 503 **ฟื้นได้** เมื่อวาง token ใหม่ ไม่ crash ไม่ทำลายข้อมูล
- public key baked — ลูกค้าอ่านโค้ดได้แต่ปลอม token ไม่ได้ (มีแค่ public key)
- clock guard กันหมุนนาฬิกาย้อน (high-water mark + tolerance 24 ชม.)

## การรับ token ใหม่ตอนต่อ MA

`Start()` มี goroutine เช็คซ้ำทุก `RecheckInterval` (default 1 นาที) และ **อ่านไฟล์ token ใหม่ทุกรอบ**
เมื่อลูกค้า apply Secret ใหม่ จะถูกหยิบมาใช้ภายในช่วงนั้นเอง โดยไม่ต้อง restart
(สอดคล้องกับการ propagate ของ k8s Secret ที่ ~1 นาทีอยู่แล้ว — ถ้าต้องการเร็วกว่านี้ค่อยเพิ่ม fsnotify)

## endpoint ใน main.go (ตัวอย่าง)

| Path | ผ่าน gate? |
|---|---|
| `GET /health` | ไม่ (health check) |
| `GET /license/status` | ไม่ (โชว์สถานะ) |
| `GET /api/work` | ใช่ (ตัวอย่าง business route) |

## build / run

```bash
go build ./...
go run .
```
