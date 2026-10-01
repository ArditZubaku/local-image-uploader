# Local Image Uploader

A Golang HTTP server for moving files between your phone and laptop over the same Wi-Fi network, in either direction:

- **Upload (phone → PC):** pick files or a folder on your phone, send them to your laptop.
- **Browse/Download (PC → phone):** browse a folder on your laptop from your phone and pull files (or a whole folder, zipped) back down.

Transfers are streamed straight to disk in both directions — memory use stays constant whether you move one photo or a 10GB folder.

The UI is optimized for iPhone Safari and desktop browsers.
Multi-select from the iOS Photo Library works reliably.

---

## Features

- Runs as a single Go binary
- No external dependencies (standard library only)
- Streams uploads directly to disk — no size limit, no buffering in memory
- Uploads sent as several parallel requests, so one phone can saturate the link
- Skips files already on the PC (same path and size), making a re-sync near-instant
- Folder upload with relative folder structure preserved
- Live progress bar with transfer speed and ETA, plus cancel and automatic retry
- Files keep their original names (` (2)` only on a real collision)
- Browse page to navigate folders on the PC from your phone
- Photo grid with cached thumbnails, so browsing a folder costs kilobytes, not gigabytes
- Multi-select any mix of files and folders and pull them down as one `.zip`
- Download single files (with resume support via HTTP Range)
- Download an entire folder as a streamed `.zip`, at any size — with a real
  `Content-Length`, so the phone shows a progress bar and an ETA
- Interrupted `.zip` downloads resume instead of restarting
- Minimal, responsive mobile UI
- Large upload button and centered layout
- iPhone-compatible multi-image selection
- Preview selected files before upload (file name + size)
- Saved file names are shown inside a styled results box
- Filenames wrap correctly (no overflow)

---

## Visual Walkthrough

### 1. Initial Screen

When you open the URL on your phone, you see the main upload card centered on the screen.
The box is fully tappable and opens the photo picker.

![Initial UI on phone](docs/images/49AE6F4B-60D3-437D-B24E-141B7F1F0B8E.png)

---

### 2. After Selecting Images (Before Upload)

When you pick one or more images from the photo library and tap **Add**, the UI updates:

- The box text changes to `1 files selected` (or `N files selected`).
- A list of selected files is shown below the box, including file name and size.
- The **Upload** button is still available.

Example with one image selected:

![UI after selecting one image](docs/images/9E50691B-33A9-4413-906D-AEE92628DC40.png)

If you select multiple images, all of them will be listed here before you upload.

---

### 3. After Uploading – Saved Files List

When you press **Upload**, the server streams the files to disk as they arrive.

- A progress bar shows percent complete and bytes transferred.
- The page does **not** reload — the upload runs in the background via the browser.
- Once done, a **Saved files:** section appears inside the card and the selection resets.
- Each saved file name is listed and wrapped correctly so it stays inside the box.

Example after uploading one file:

![UI after upload with saved files list](docs/images/5875F1A2-7625-4EE5-AFDD-E6F196EB9D2F.png)

The names are prefixed with a timestamp, ensuring uniqueness.

---

### 4. Files on Disk in Your Editor

On your laptop, the uploaded files are stored in the `uploads` directory.

Example view in VS Code:

- Left: project tree showing `uploads/` and your image file.
- Right: the actual image opened in the editor.

Uploaded files are stored in:

```text
./uploads/<timestamp>_<filename>.jpg
```

If you upload a folder, its structure is preserved underneath `uploads/`, with only the final filename timestamp-prefixed:

```text
./uploads/<subfolder>/<timestamp>_<filename>.jpg
```

Each filename is prefixed with a unique timestamp to avoid collisions.

![VS Code showing project and uploaded image](docs/images/2A2B92D1-0FC3-41AB-9FEA-08BBAF6522EB.png)

You can open the file, move it, rename it, or use it anywhere else.

---

### 5. Browsing and Downloading (PC → Phone)

Tap **Browse** (next to **Upload** in the nav row) to go the other way: pull files from your laptop down to your phone.

- Images show as a thumbnail grid by default; the **List**/**Grid** button switches
  views and the choice is remembered. Thumbnails are generated once and cached, so a
  folder of 60 photos loads in about a megabyte instead of a few hundred.
- Folders are listed first, then files, each with its size.
- Tap a folder name to open it; breadcrumbs at the top let you go back up.
- Tap a photo (or **Get** on a file) to pull the full-size original — images open
  inline so iOS Safari's long-press **Save to Photos** still works; other file types
  download as usual.
- Tick any mix of files and folders and tap **Download N items (.zip)** to pull just
  that selection.
- Use **Download folder (.zip)** at the top to pull everything in the current folder.
  Archives are zipped on the fly as they stream, so this works for folders much larger
  than your phone's free RAM, and the exact size is sent up front so the phone can show
  real progress.
- If a `.zip` download is cancelled or the phone drops off Wi-Fi, resuming it continues
  where it stopped rather than starting over. The archive is rebuilt locally up to that
  point (fast, off disk) but only the remaining bytes are sent over the network. If any
  file in the folder changed in the meantime the resume is refused and the download
  simply restarts, so a part-finished archive can never be spliced to a different one.

By default, Browse shows the same `uploads` directory files land in, so anything you (or someone else) uploads is immediately available to pull back down. Point it at a different folder with `IMAGEDROP_SHARE_DIR` (see Configuration below).

---

## Running the Server

#### Requirements

- Go 1.21 or newer
- Laptop and phone must be on the same Wi-Fi network
- Works on:
  - iPhone Safari
  - Chrome (Android / Windows / macOS)
  - Firefox
  - Edge

From the project root:

```bash
go run .
```

Expected output in the terminal:

```text
Server listening on:
-> http://192.168.0.33:8080/

Open one of the URLs above in your phone browser (same Wi-Fi).
```

Example (real output):

![Terminal output showing server and requests](docs/images/53CDBC6F-0A60-4AAB-8804-583AC156198E.png)

Copy the printed URL (for example `http://192.168.0.33:8080/`) into your phone browser.

---

## Configuration (Optional)

You may configure runtime behavior using environment variables.

### Available variables

| Variable                | Description                  | Default  |
|-------------------------|------------------------------|----------|
| `IMAGEDROP_ADDR`        | Listen address/port          | `:8080`  |
| `IMAGEDROP_UPLOAD_DIR`  | Directory to save uploads    | `uploads`|
| `IMAGEDROP_SHARE_DIR`   | Directory exposed by the Browse/Download page | same as `IMAGEDROP_UPLOAD_DIR` |
| `IMAGEDROP_THUMB_DIR`   | Where generated thumbnails are cached; `off` disables previews | OS cache dir, e.g. `~/Library/Caches/imagedrop/thumbs` |
| `IMAGEDROP_MAX_UPLOAD_MB` | Max upload request size (MB); unset or `0` means unlimited (disk space is the only limit) | `0` (unlimited) |

### Example (PowerShell)

```powershell
$env:IMAGEDROP_ADDR=":9090"
$env:IMAGEDROP_UPLOAD_DIR="D:\photos"
$env:IMAGEDROP_MAX_UPLOAD_MB="200"

go run .
```

### Example (Windows cmd.exe)

```cmd
set IMAGEDROP_ADDR=:9090
set IMAGEDROP_UPLOAD_DIR=D:\photos
set IMAGEDROP_MAX_UPLOAD_MB=200

go run .
```

Both examples above set an explicit 200MB cap; omit the variable (or set it to `0`) to leave uploads unlimited.

---

## Notes

- Designed for local network use only.
- Do **not** expose this server directly to the public internet — the Browse page has no authentication, so anyone on the network who can reach the server can read `IMAGEDROP_SHARE_DIR`.
- Uploads are unlimited by default (bounded only by free disk space); set `IMAGEDROP_MAX_UPLOAD_MB` for a cap.
- No server-side time limit on a transfer in either direction — large files or folders just take as long as your Wi-Fi speed allows.
