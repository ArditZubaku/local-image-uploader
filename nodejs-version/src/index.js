const express = require("express");
const multer = require("multer");
const fs = require("fs");
const path = require("path");
const os = require("os");

const PORT = parseInt(process.env.PORT || "8080", 10);
const UPLOAD_DIR = process.env.UPLOAD_DIR || path.join(__dirname, "..", "uploads");
const MAX_UPLOAD_MB = parseInt(process.env.MAX_UPLOAD_MB || "100", 10);

// Ensure upload directory exists
fs.mkdirSync(UPLOAD_DIR, { recursive: true });

const storage = multer.diskStorage({
  destination: function (_req, _file, cb) {
    cb(null, UPLOAD_DIR);
  },
  filename: function (_req, file, cb) {
    const timestamp = Date.now();
    const safeName = path.basename(file.originalname).replace(/[^\w.\-]/g, "_");
    cb(null, `${timestamp}_${safeName}`);
  },
});

const upload = multer({
  storage,
  limits: {
    fileSize: MAX_UPLOAD_MB * 1024 * 1024, // per file
  },
});

const app = express();

// Serve static UI
app.use(express.static(path.join(__dirname, "..", "public")));

// Health check
app.get("/healthz", (_req, res) => {
  res.status(200).send("ok");
});

// Handle uploads via AJAX
app.post("/upload", upload.array("files"), (req, res) => {
  try {
    if (!req.files || req.files.length === 0) {
      return res.status(400).json({ error: "no files uploaded" });
    }

    const savedFiles = req.files.map((f) => path.basename(f.filename));
    return res.json({ savedFiles });
  } catch (err) {
    console.error("upload error:", err);
    return res.status(500).json({ error: "internal upload error" });
  }
});

// Helper to compute LAN URLs
function getLanUrls(port) {
  const ifaces = os.networkInterfaces();
  const urls = [];

  for (const name of Object.keys(ifaces)) {
    for (const addr of ifaces[name] || []) {
      if (addr.family !== "IPv4" || addr.internal) continue;
      if (!isPrivateIPv4(addr.address)) continue;
      urls.push(`http://${addr.address}:${port}/`);
    }
  }

  if (urls.length === 0) {
    urls.push(`http://127.0.0.1:${port}/`);
  }

  return urls;
}

function isPrivateIPv4(ip) {
  const parts = ip.split(".").map((p) => parseInt(p, 10));
  if (parts.length !== 4) return false;
  if (parts[0] === 10) return true;
  if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) return true;
  if (parts[0] === 192 && parts[1] === 168) return true;
  return false;
}

app.listen(PORT, () => {
  console.log("Server listening on:");
  const urls = getLanUrls(PORT);
  for (const url of urls) {
    console.log(`-> ${url}`);
  }
  console.log("\nOpen one of the URLs above in your phone browser (same Wi-Fi).");
});
