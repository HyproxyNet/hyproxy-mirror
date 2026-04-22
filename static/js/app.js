'use strict';

// ============================================================
// Detection helpers — universal rules for all repos
// ============================================================

/**
 * Detect the operating system from a release asset filename.
 * Rules cover all repos in repos.json (sing-box, clash-party,
 * hiddify, CMFA, v2rayN/NG, surfboard, clash-verge-rev, FlClash).
 */
function detectOS(name) {
  const n = name.toLowerCase();

  // OpenWrt first — it is "linux" too but more specific
  if (/\bopenwrt\b/.test(n)) return 'openwrt';

  // Android — check before Linux
  if (/\bandroid\b/.test(n)) return 'android';

  // Windows — win7 is a variant, not a separate OS
  if (/\bwindows\b|[-_]win7[-_]/.test(n)) return 'windows';

  // macOS — darwin (Go cross-compile name), catalina (macOS 10.15 variant in clash-party), apple (SFM-*-Apple.pkg)
  if (/\bdarwin\b|\bmacos\b|\bcatalina\b|\bapple\b/.test(n)) return 'macos';

  // Linux
  if (/\blinux\b|\bdebian\b/.test(n)) return 'linux';

  // Extension-based fallbacks for ambiguous names
  if (/\.apk$/i.test(name))                        return 'android';
  if (/\.(exe|msix)$/i.test(name))                 return 'windows';
  if (/\.(dmg|pkg)$/i.test(name))                  return 'macos';
  if (/dsyms/i.test(name))                         return 'macos'; // Apple dSYMs debug symbols
  if (/\.ipk$/i.test(name))                        return 'openwrt';
  if (/\.(deb|rpm|appimage)$/i.test(name))         return 'linux';

  return 'other';
}

/**
 * Detect CPU architecture from a release asset filename.
 *
 * Priority: universal > arm64 > armv7 > x64 > x86 > other
 *
 * Key design decisions:
 *  - arm64 is checked BEFORE generic "64" patterns so "arm64" is never
 *    misclassified as x64.
 *  - Standalone "-64" (v2rayN-macos-64.dmg) is detected as x64.
 *  - OpenWrt files with exotic cortex/MIPS variants fall into 'other'
 *    and are shown whenever OS=openwrt regardless of arch filter.
 */
function detectArch(name) {
  const n = name.toLowerCase();

  // Universal build (works on any arch)
  if (/\buniversal\b/.test(n)) return 'universal';

  // ARM 64-bit: aarch64, arm64 (and compound forms like arm64-v8a)
  if (/aarch64|arm64/.test(n)) return 'arm64';

  // ARM 32-bit / ARMv7
  // armeabi-v7a, armv7, armhf/armhfp (rpm), arm7 (Hiddify naming), armv7hl (rpm)
  if (/armeabi-v7a|armv7|armhf|arm7\b|armv7hl/.test(n)) return 'armv7';

  // x86-64 / AMD64
  // x86_64, x86-64, amd64, \bx64\b as a whole word
  if (/x86[_-]64|amd64|\bx64\b/.test(n)) return 'x64';

  // Standalone "-64" / "_64" / ".64" pattern used by some repos (v2rayN, hiddify)
  // The regex requires a separator before 64 so it cannot fire on "arm64".
  if (/[-_.]64(?=[-_.]|$)/.test(n) && !/arm64/.test(n)) return 'x64';

  // x86 / 32-bit Intel: x86 (but not x86_64), i386, 386, ia32, pentium
  if (/\bx86\b(?![_-]64)|\bi386\b|\b386\b|\bia32\b|\bpentium\b/.test(n)) return 'x86';

  // Everything else (MIPS, RISC-V, LoongArch, PowerPC, s390x, OpenWrt cortex variants…)
  return 'other';
}

/**
 * Returns true for checksum, signature and metadata files that are
 * not direct downloads (hidden by default).
 */
function isMeta(name) {
  const n = name.toLowerCase();
  return (
    n.endsWith('.sha256') ||
    n.endsWith('.sig')    ||
    n.endsWith('.json')   ||
    n.endsWith('.yml')    ||
    n.endsWith('.yaml')   ||
    /metadata|latest\./i.test(name)
  );
}

/**
 * Returns the appropriate FontAwesome class for a file, based on
 * extension and known naming conventions.
 */
function getIcon(name) {
  const n = name.toLowerCase();
  if (n.endsWith('.apk'))                               return 'fab fa-android';
  if (n.endsWith('.exe') || n.endsWith('.msix'))        return 'fab fa-windows';
  if (n.endsWith('.dmg') || n.endsWith('.pkg') || n.includes('dsyms')) return 'fab fa-apple';
  if (n.endsWith('.deb') || n.endsWith('.rpm'))         return 'fab fa-linux';
  if (n.endsWith('.appimage'))                          return 'fab fa-linux';
  if (n.endsWith('.ipk'))                               return 'fas fa-network-wired';
  if (n.endsWith('.zip')   || n.endsWith('.7z') ||
      n.endsWith('.tar.gz')|| n.endsWith('.tgz') ||
      n.endsWith('.tar.zst'))                           return 'fas fa-file-zipper';
  if (n.endsWith('.sha256'))                            return 'fas fa-shield-halved';
  if (n.endsWith('.sig'))                               return 'fas fa-certificate';
  if (n.endsWith('.json') || n.endsWith('.yml') || n.endsWith('.yaml')) return 'fas fa-file-code';
  return 'fas fa-file-arrow-down';
}

// ============================================================
// Filter state
// ============================================================

const state = {
  software: '',
  os:       '',
  arch:     '',
  showMeta: false,
};

// ============================================================
// DOM initialisation
// ============================================================

function initItems() {
  document.querySelectorAll('.asset-item').forEach(item => {
    const name = item.dataset.name;

    // Annotate with detected OS / arch / meta flag
    item.dataset.os   = detectOS(name);
    item.dataset.arch = detectArch(name);
    item.dataset.meta = isMeta(name) ? 'true' : 'false';

    // Replace placeholder icon
    const iconEl = item.querySelector('.asset-icon');
    if (iconEl) iconEl.className = 'asset-icon ' + getIcon(name);
  });
}

function initSoftwareDropdown() {
  const select = document.getElementById('filterSoftware');
  if (!select) return;

  document.querySelectorAll('.repo-card-wrapper[data-repo]').forEach(card => {
    const repo = card.dataset.repo;
    const opt  = document.createElement('option');
    opt.value       = repo;
    // Display just the repo part (after "/"), e.g. "sing-box"
    opt.textContent = repo.includes('/') ? repo.split('/')[1] : repo;
    select.appendChild(opt);
  });
}

// ============================================================
// Filter logic
// ============================================================

/**
 * Returns true if the item should be visible given the current state.
 * OpenWrt items ignore the arch filter (users must pick the exact
 * chipset variant themselves once the OS list is narrowed).
 */
function itemVisible(item) {
  const meta   = item.dataset.meta === 'true';
  const itemOS = item.dataset.os;
  const itemArch = item.dataset.arch;

  // Meta files
  if (meta && !state.showMeta) return false;

  // OS filter
  if (state.os && itemOS !== state.os) return false;

  // Arch filter — skipped for OpenWrt; also always pass 'universal'
  // and 'other' if the OS already matches (handles universal DMGs etc.)
  if (state.arch) {
    const isOpenwrt   = itemOS === 'openwrt';
    const isUniversal = itemArch === 'universal';
    const isOther     = itemArch === 'other';
    const osMatch     = state.os ? itemOS === state.os : true;

    if (!isOpenwrt && !isUniversal && !(isOther && osMatch) && itemArch !== state.arch) {
      return false;
    }
  }

  return true;
}

function applyFilters() {
  const isFiltering = !!(state.software || state.os || state.arch);
  const container   = document.getElementById('repoContainer');
  container?.classList.toggle('filtering', isFiltering);

  let visibleFiles  = 0;
  let totalFiles    = 0;
  let visibleCards  = 0;

  document.querySelectorAll('.repo-card-wrapper').forEach(card => {
    const repo = card.dataset.repo;

    // Software filter hides entire cards
    if (state.software && repo !== state.software) {
      card.style.display = 'none';
      return;
    }

    let cardHasVisible = false;

    card.querySelectorAll('.asset-item').forEach(item => {
      const meta = item.dataset.meta === 'true';
      if (!meta) totalFiles++;

      const show = itemVisible(item);
      item.style.display = show ? '' : 'none';

      if (show) {
        cardHasVisible = true;
        if (!meta) visibleFiles++;
      }
    });

    // Hide card when filtering produces no matches
    const hide = isFiltering && !cardHasVisible;
    card.style.display = hide ? 'none' : '';

    if (!hide) {
      visibleCards++;
      // Auto-expand cards when a filter is active
      if (isFiltering) {
        card.querySelector('.collapsible-content')?.classList.add('open');
        card.querySelector('.chevron-icon')?.classList.add('rotated');
      }
    }
  });

  // No-results banner
  const noResults = document.getElementById('noResults');
  if (noResults) {
    noResults.style.display = (isFiltering && visibleFiles === 0) ? 'block' : 'none';
  }

  updateStats(visibleFiles, totalFiles, visibleCards, isFiltering);
  syncPresetButtons();
}

function updateStats(visible, total, cards, isFiltering) {
  const el = document.getElementById('filterStats');
  if (!el) return;
  if (!isFiltering) {
    const totalCards = document.querySelectorAll('.repo-card-wrapper').length;
    el.textContent = `共 ${total} 个文件，${totalCards} 个仓库`;
  } else {
    el.textContent = `找到 ${visible} 个文件，来自 ${cards} 个仓库`;
  }
}

function syncPresetButtons() {
  document.querySelectorAll('.preset-btn').forEach(btn => {
    btn.classList.toggle(
      'active',
      btn.dataset.os === state.os && btn.dataset.arch === state.arch
    );
  });
}

// ============================================================
// Event wiring
// ============================================================

function initFilters() {
  const selSoftware = document.getElementById('filterSoftware');
  const selOS       = document.getElementById('filterOS');
  const selArch     = document.getElementById('filterArch');
  const chkMeta     = document.getElementById('showMeta');
  const btnReset    = document.getElementById('resetFilter');

  selSoftware?.addEventListener('change', e => { state.software = e.target.value; applyFilters(); });
  selOS?.addEventListener(      'change', e => { state.os       = e.target.value; applyFilters(); });
  selArch?.addEventListener(    'change', e => { state.arch     = e.target.value; applyFilters(); });
  chkMeta?.addEventListener(    'change', e => { state.showMeta = e.target.checked; applyFilters(); });

  btnReset?.addEventListener('click', () => {
    state.software = state.os = state.arch = '';
    state.showMeta = false;

    if (selSoftware) selSoftware.value = '';
    if (selOS)       selOS.value       = '';
    if (selArch)     selArch.value     = '';
    if (chkMeta)     chkMeta.checked   = false;

    // Collapse all cards
    document.querySelectorAll('.collapsible-content').forEach(c => c.classList.remove('open'));
    document.querySelectorAll('.chevron-icon').forEach(c => c.classList.remove('rotated'));

    applyFilters();
  });

  // Quick-select preset buttons
  document.querySelectorAll('.preset-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const os   = btn.dataset.os;
      const arch = btn.dataset.arch;

      // Clicking the active preset toggles it off
      if (state.os === os && state.arch === arch) {
        state.os = state.arch = '';
        if (selOS)   selOS.value   = '';
        if (selArch) selArch.value = '';
      } else {
        state.os   = os;
        state.arch = arch;
        if (selOS)   selOS.value   = os;
        if (selArch) selArch.value = arch;
      }

      applyFilters();
    });
  });
}

// ============================================================
// Collapsible cards — one open at a time (accordion)
// ============================================================

function initCollapsible() {
  document.querySelectorAll('.collapsible-header').forEach(header => {
    header.addEventListener('click', () => {
      const content = header.nextElementSibling;
      const chevron = header.querySelector('.chevron-icon');
      if (!content) return;

      const opening = !content.classList.contains('open');

      // Close all others
      document.querySelectorAll('.collapsible-content.open').forEach(c => {
        if (c !== content) {
          c.classList.remove('open');
          c.previousElementSibling?.querySelector('.chevron-icon')?.classList.remove('rotated');
        }
      });

      content.classList.toggle('open', opening);
      chevron?.classList.toggle('rotated', opening);
    });
  });
}

// ============================================================
// Dark mode
// ============================================================

function initDarkMode() {
  const toggle = document.getElementById('darkModeToggle');
  const icon   = toggle?.querySelector('i');

  function setDark(dark) {
    document.body.classList.toggle('dark-mode', dark);
    if (icon) icon.className = dark ? 'fas fa-sun' : 'fas fa-moon';
    localStorage.setItem('darkMode', dark ? '1' : '');
  }

  if (localStorage.getItem('darkMode') === '1') setDark(true);

  toggle?.addEventListener('click', () =>
    setDark(!document.body.classList.contains('dark-mode'))
  );
}

// ============================================================
// Entry point
// ============================================================

document.addEventListener('DOMContentLoaded', () => {
  initDarkMode();
  initItems();
  initSoftwareDropdown();
  initCollapsible();
  initFilters();
  applyFilters(); // populate initial stats
});
