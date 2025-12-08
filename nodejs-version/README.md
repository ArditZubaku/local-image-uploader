# Local Image Uploader (Node.js)

This is a Node.js version of the local image uploader.
It lets you upload images from your phone to your laptop over the same Wi‑Fi network using a simple web UI.

The UI is similar to the Go version:

- Centered card
- Large tap area for choosing images
- Preview list of selected files
- Saved files list after upload

## Requirements

- Node.js 18 or newer
- npm
- Phone and laptop on the same Wi‑Fi network

## Install

```bash
npm install
```

## Run

```bash
npm start
```

You should see something like:

```text
Server listening on:
-> http://192.168.0.33:8080/

Open one of the URLs above in your phone browser (same Wi-Fi).
```

Open that URL on your phone.

## Usage

1. Tap the big box to choose images from your photo library.
2. Selected files are listed with name and size.
3. Tap **Upload**.
4. The server saves each file under the `uploads` directory, prefixed with a timestamp.
5. The UI shows a **Saved files** list with the stored filenames.

## Configuration

Environment variables:

- `PORT` – HTTP port (default: `8080`)
- `UPLOAD_DIR` – directory for uploads (default: `./uploads` relative to project root)
- `MAX_UPLOAD_MB` – per-file size limit in megabytes (default: `100`)

Example on Linux/macOS:

```bash
PORT=9000 UPLOAD_DIR=/tmp/uploads MAX_UPLOAD_MB=50 npm start
```

On Windows PowerShell:

```powershell
$env:PORT="9000"
$env:UPLOAD_DIR="D:\uploads"
$env:MAX_UPLOAD_MB="50"
npm start
```

## Notes

- This server is meant for local network use only.
- Do not expose it directly to the internet.
