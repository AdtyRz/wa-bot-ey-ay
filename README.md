# TemanWA AI - Bot WhatsApp AI Bergaya Gen Z

TemanWA AI adalah bot WhatsApp personal berbasis AI dengan gaya bahasa Indonesia kasual, santai (10% cuek, 10% humor ringan), responsif, empatik saat curhat, dan memahami konteks percakapan serta pesan multimodal.

Proyek ini dibangun berdasarkan spesifikasi lengkap pada `PRD_Bot_WhatsApp_AI_GenZ_GoWA.md`.

---

## 🛠️ Tech Stack

- **Go (Golang 1.24)** - Core engine, concurrency worker pool, orchestrator, dan command handling.
- **GOWA (Go WhatsApp Web Multi-Device)** - WhatsApp connector via Webhook & REST API.
- **Gemini API** - Chat reasoning, multimodal vision (gambar/stiker/audio/dokumen) dengan mode anggaran `strict_free`.
- **Supabase PostgreSQL (`pgx/v5`)** - Riwayat pesan, ringkasan memori, preferensi, dan idempotensi deduplikasi event. (Mendukung in-memory fallback otomatis jika DB belum aktif).
- **FFmpeg / FFprobe** - Ekstraksi media & validasi durasi (opsional).

---

## 📁 Struktur Direktori

```text
├── cmd/
│   └── bot/
│       └── main.go          # Entry point & graceful shutdown
├── internal/
│   ├── ai/
│   │   ├── gemini/          # Client Gemini API & quota guard
│   │   ├── prompts/         # Persona Gen Z & context formatter
│   │   └── trends/          # Adapter tren viral & grounding
│   ├── commands/            # Command handler (.ping, .menu, .reset, dll.)
│   ├── config/              # Validasi konfigurasi environment
│   ├── conversation/        # Orchestrator alur percakapan
│   ├── media/               # Validasi media, TTL cleanup, ffmpeg
│   ├── observability/       # Structured logger (slog) & redaksi data sensitif
│   ├── queue/               # Bounded worker pool & backpressure
│   ├── storage/
│   │   ├── postgres/        # Repository Supabase PostgreSQL
│   │   ├── memory_repo.go   # In-memory repository fallback
│   │   └── repository.go    # Interface & domain model
│   ├── webhook/             # HMAC-SHA256 verification & payload normalizer
│   └── whatsapp/            # GOWA REST client
├── migrations/
│   └── 001_initial_schema.sql # Skema database Supabase
├── prompts/
│   └── persona_id.md        # Definisi persona sistem Gen Z
├── .env.example
├── .gitignore
├── go.mod
└── go.sum
```

---

## 🚀 Cara Menjalankan

### 1. Salin dan Sesuaikan Konfigurasi Environment

```bash
cp .env.example .env
```

Isi variabel penting pada `.env`:
- `GEMINI_API_KEY`: Kunci API Gemini Anda.
- `GOWA_BASE_URL`: URL instance GOWA (default: `http://localhost:3000`).
- `GOWA_WEBHOOK_SECRET`: Secret HMAC jika mengaktifkan verifikasi webhook.
- `SUPABASE_DB_URL`: Connection string PostgreSQL Supabase (jika kosong, bot otomatis fallback ke in-memory storage).

### 2. Jalankan Migrasi Database (Opsional bila menggunakan Supabase)

Eksekusi file `migrations/001_initial_schema.sql` di Supabase SQL Editor.

### 3. Kompilasi dan Jalankan Bot

```bash
go build -o bin/temanwa-ai ./cmd/bot
./bin/temanwa-ai
```

---

## 💬 Fitur Utama & Perilaku AI

1. **Gaya Bahasa Kasual & Singkat:** Default 1–3 kalimat, menggunakan bahasa santai tanpa terdengar kaku seperti CS.
2. **Penanganan Khusus Pesan Video:** Sesuai instruksi, AI tidak memproses video secara berat melainkan otomatis membalas dengan alasan santai/lucu (seperti `"adugh memorinya abis ga bisa buka video 😭"`, `"video apa ini ga bisa di buka wkwk, hp kentang gue ngelag"`).
3. **Pesan Multimodal Lainnya:** Memahami gambar/foto, voice note/audio, stiker, dan dokumen dalam batas ukuran aman.
4. **Idempotensi & Anti-Loop:** Pesan duplikat tidak akan menghasilkan balasan ganda, dan pesan dari bot sendiri (`is_from_me=true`) otomatis diabaikan.
5. **Command Kontrol:**
   - `.menu` / `.help` : Bantuan perintah.
   - `.ping` : Cek koneksi bot.
   - `.status` : Cek status bot.
   - `.clear` : Bersihkan riwayat chat sesi aktif.
   - `.mute` : Matikan respons bot di chat/grup ini.
   - `.unmute` : Aktifkan kembali respons bot di chat/grup ini.
   - `.hapusdata` : Hapus seluruh ingatan personal pengguna.
6. **Mute / Blacklist Grup:** Grup dapat di-mute langsung lewat chat (`mute grup ini`, `jangan balas di grup ini`, `.mute`) atau via `.env` (`BOT_BLOCKED_CHATS=jid1,jid2`). Bot tidak akan pernah mengirimkan pesan sekalipun di grup tersebut.
7. **Kirim Pesan & Auto-Forward ke Owner:** Owner dapat memerintahkan bot mengirim pesan ke kontak (`kirim pesan kepada [nama/nomor] [pesan]`), dan saat kontak tersebut membalas, pesan balasannya otomatis diteruskan ke WhatsApp owner.

---

## 🧪 Menjalankan Pengujian (Unit & Acceptance Tests)

Seluruh skenario pengujian kritis (AT-01 hingga AT-20) dapat dijalankan dengan:

```bash
go test -v ./...
```
