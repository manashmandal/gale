import puppeteer from "puppeteer";
import { mkdir, rm } from "fs/promises";
import { existsSync } from "fs";
import { $ } from "bun";

const ANIMATION_DURATION = 10000; // 10 seconds (one full loop - timeline goes to 9.5s)
const FPS = 12;
const FRAME_COUNT = Math.floor((ANIMATION_DURATION / 1000) * FPS);
const FRAME_DELAY = 1000 / FPS;
const OUTPUT_SIZE = 1080; // LinkedIn-friendly size

async function captureAnimation() {
  const framesDir = "./frames";

  // Clean up previous frames
  if (existsSync(framesDir)) {
    await rm(framesDir, { recursive: true });
  }
  await mkdir(framesDir);

  console.log("🚀 Launching browser...");
  const browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"],
  });

  const page = await browser.newPage();
  await page.setViewport({ width: 1080, height: 1080, deviceScaleFactor: 1 });

  console.log("📄 Loading animation...");
  await page.goto(`file://${process.cwd()}/gale-animation.html`, {
    waitUntil: "networkidle0",
  });

  // Wait for initial animation setup
  await new Promise((r) => setTimeout(r, 500));

  console.log(`📸 Capturing ${FRAME_COUNT} frames at ${FPS} FPS...`);

  for (let i = 0; i < FRAME_COUNT; i++) {
    const frameNumber = String(i).padStart(4, "0");
    await page.screenshot({
      path: `${framesDir}/frame_${frameNumber}.png`,
      clip: { x: 0, y: 0, width: 1080, height: 1080 },
    });

    if (i % 10 === 0) {
      console.log(`  Frame ${i + 1}/${FRAME_COUNT}`);
    }

    await new Promise((r) => setTimeout(r, FRAME_DELAY));
  }

  await browser.close();
  console.log("✅ Frames captured!");

  // Generate GIF using ffmpeg
  console.log("🎬 Generating GIF...");

  const outputFile = "gale-animation.gif";

  // Create optimized GIF with good quality palette
  await $`ffmpeg -y -framerate ${FPS} -i ${framesDir}/frame_%04d.png -vf "fps=${FPS},scale=${OUTPUT_SIZE}:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128:stats_mode=diff[p];[s1][p]paletteuse=dither=bayer:bayer_scale=3" -loop 0 ${outputFile}`;

  console.log(`✅ GIF created: ${outputFile}`);

  // Get file size
  const stat = await Bun.file(outputFile).size;
  console.log(`📦 File size: ${(stat / 1024 / 1024).toFixed(2)} MB`);

  // Clean up frames
  await rm(framesDir, { recursive: true });
  console.log("🧹 Cleaned up frames");

  console.log("\n🎉 Done! Your LinkedIn animation is ready.");
}

captureAnimation().catch(console.error);
