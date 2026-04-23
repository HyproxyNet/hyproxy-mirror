'use strict';

// ============================================================
// Detection helpers — universal rules for all repos
// ============================================================

/**
 * Tokenise a filename on the separators used by release-asset naming
 * conventions: `-`, `_`, `.`, `/`, and whitespace.
 *
 * Why: `\b` word boundaries don't work here — the underscore is a
 * regex "word character", so `\bopenwrt\b` misses `_openwrt_` and
 * sing-box's `*_openwrt_*.apk` Alpine packages get misclassified as
 * Android by the `.apk` fallback. Splitting explicitly on separators
 * and comparing tokens avoids that whole class of bug.
 */
function tokenize(name) {
  return name.toLowerCase().split(/[-_.\/\s]+/).filter(Boolean);
}

/**
 * Detect the operating system from a release asset filename.
 * Rules cover all repos in repos.json (sing-box, clash-party,
 * hiddify, CMFA, v2rayN/NG, surfboard, clash-verge-rev, FlClash).
 *
 * Strategy: explicit OS tokens win over extension fallbacks. This
 * disambiguates `.apk` (Android APK vs Alpine Linux/OpenWrt package)
 * and `.exe` (generic Windows vs win7 variant).
 */
function detectOS(name) {
  const n = name.toLowerCase();
  const tokens = tokenize(name);
  const has = t => tokens.includes(t);

  // Priority 1: explicit OS tokens (most specific first)
  if (has('openwrt'))                                return 'openwrt';
  if (has('android'))                                return 'android';
  if (has('windows') || has('win') || has('win7'))   return 'windows';
  if (has('darwin') || has('macos') || has('catalina')
      || has('apple') || has('intel')
      || /\.app\.tar\.gz(\.sig)?$/.test(n))          return 'macos';
  if (has('linux') || has('debian'))                 return 'linux';

  // Priority 2: extension fallbacks (for names with no OS token,
  // e.g. SFA-1.13.11-x86_64.apk, cmfa-*-arm64-v8a-release.apk)
  if (n.endsWith('.ipk'))                            return 'openwrt';
  if (n.endsWith('.apk'))                            return 'android';
  if (n.endsWith('.exe') || n.endsWith('.msix'))     return 'windows';
  if (n.endsWith('.dmg') || n.endsWith('.pkg')
      || /dsyms/.test(n))                            return 'macos';
  if (n.endsWith('.deb') || n.endsWith('.rpm')
      || n.endsWith('.appimage'))                    return 'linux';

  return 'other';
}

/**
 * Detect CPU architecture from a release asset filename.
 *
 * Priority: universal > arm64 > armv7 > x64 > x86 > other
 *
 * Uses the same token-based approach as detectOS so names like
 * `sing-box_1.13.11_linux_x86_64.apk` (tokens: x86, 64) or
 * `v2rayN-macos-64.dmg` (standalone 64) resolve correctly without
 * fighting word-boundary rules.
 */
function detectArch(name) {
  const tokens = tokenize(name);
  const has = t => tokens.includes(t);

  // Universal build (works on any arch)
  if (has('universal')) return 'universal';

  // ARM 64-bit: aarch64, arm64 (compound "arm64-v8a" splits to 'arm64' + 'v8a')
  if (has('aarch64') || has('arm64')) return 'arm64';

  // ARM 32-bit / ARMv7 family
  // armv7, armv7a, armv7hl, armhf, armhfp, arm7 (Hiddify), armeabi (split from armeabi-v7a),
  // armel/armv6/armv6hl — 32-bit soft-float variants grouped here as the closest match
  if (has('armv7') || has('armv7a') || has('armv7hl')
      || has('armhf') || has('armhfp')
      || has('arm7') || has('armeabi')
      || has('armel') || has('armv6') || has('armv6hl')) return 'armv7';

  // x86-64 / AMD64
  // amd64, x64, or the split "x86_64"/"x86-64" which tokenises to ['x86', '64']
  if (has('amd64') || has('x64')) return 'x64';
  if (has('x86') && has('64')) return 'x64';

  // Standalone "-64" / "_64" / ".64" (v2rayN-linux-64.zip, v2rayN-macos-64.dmg)
  // Safe because arm64/aarch64 were already handled above.
  if (has('64')) return 'x64';

  // x86 / 32-bit Intel
  if (has('x86') || has('i386') || has('386')
      || has('ia32') || has('pentium') || has('pentium4')) return 'x86';

  // Everything else (MIPS, RISC-V, LoongArch, PowerPC, s390x, armv5,
  // OpenWrt cortex/xscale/fa526 variants…)
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
