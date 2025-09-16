import { createPublicClient, http as viemHttp, parseAbiItem, decodeEventLog } from 'viem'
import fs from 'fs'
import path from 'path'
import { arbitrum } from 'viem/chains'
import http from 'http'

const RAW_RPC_HTTP = process.env.RPC_HTTP || process.env.ARBITRUM_HTTP_RPC || process.env.ANKR_HTTP
const ANKR_API_KEY = process.env.ANKR_API_KEY || process.env.ANKR_KEY
// Çoklu key desteği: ANKR_KEYS/ANKR_API_KEYS ("," ile) veya ANKR_KEY_1.._4 / ANKR_API_KEY_1.._4
const ANKR_KEYS_ENV = (process.env.ANKR_KEYS || '').split(',').map(s => s.trim()).filter(Boolean)
const ANKR_API_KEYS_ENV = (process.env.ANKR_API_KEYS || '').split(',').map(s => s.trim()).filter(Boolean)
const ANKR_KEYS_INDEXED = [process.env.ANKR_KEY_1, process.env.ANKR_KEY_2, process.env.ANKR_KEY_3, process.env.ANKR_KEY_4]
  .map(s => (s || '').trim()).filter(Boolean)
const ANKR_API_KEYS_INDEXED = [process.env.ANKR_API_KEY_1, process.env.ANKR_API_KEY_2, process.env.ANKR_API_KEY_3, process.env.ANKR_API_KEY_4]
  .map(s => (s || '').trim()).filter(Boolean)

let RPC_HTTP = RAW_RPC_HTTP
// Ankr için anahtar yoksa otomatik path'e ekle (https://rpc.ankr.com/arbitrum/<KEY>)
if (RPC_HTTP && /rpc\.ankr\.com\/arbitrum/i.test(RPC_HTTP)) {
  const hasKeyInPath = /rpc\.ankr\.com\/arbitrum\/[A-Za-z0-9]/i.test(RPC_HTTP)
  const hasQuery = /[?&](api_key|ankr_api_key)=/i.test(RPC_HTTP)
  if (!hasKeyInPath && !hasQuery && ANKR_API_KEY) {
    RPC_HTTP = RPC_HTTP.replace(/\/$/, '') + '/' + ANKR_API_KEY
  }
}
const NOTIFIER_URL = process.env.NOTIFIER_URL || 'http://localhost:8080/notify'
const POLL_MS = Number(process.env.POLL_MS || 5000)
const CHUNK_BLOCKS = Number(process.env.CHUNK_BLOCKS || 4000)
const CONFIRMATIONS = Number(process.env.CONFIRMATIONS || 12)
const START_BLOCK = process.env.START_BLOCK || 'latest'
const BACKFILL_BLOCKS = Number(process.env.BACKFILL_BLOCKS || 0)
const ENABLE_GENERIC_EVENTS = String(process.env.ENABLE_GENERIC_EVENTS || 'true').toLowerCase() === 'true'
let WATCH_ADDRESSES = (process.env.WATCH_ADDRESSES || '').split(',').map(a => a.trim()).filter(Boolean)
const TOKEN_LIST = (process.env.TOKEN_LIST || '').split(',').map(a => a.trim()).filter(Boolean)

// --- ANKR key rotasyonu: birden fazla HTTP client oluştur ---
function buildAnkrUrls() {
  const keys = [
    ...ANKR_KEYS_ENV,
    ...ANKR_API_KEYS_ENV,
    ...ANKR_KEYS_INDEXED,
    ...ANKR_API_KEYS_INDEXED,
  ]
  const urls = []
  if (keys.length > 0) {
    // Her key için tam URL üret
    for (const k of keys) {
      if (!k) continue
      urls.push(`https://rpc.ankr.com/arbitrum/${k}`)
    }
  }
  // Eğer tekil RPC_HTTP varsa, onu da listenin başına ekle
  if (RPC_HTTP) {
    // çıplak (anahtarsız) ankr URL'si ise ve elimizde anahtarlar varsa eklemeyelim
    const isAnkr = /rpc\.ankr\.com\/arbitrum/i.test(RPC_HTTP)
    const hasKeyInPath = /rpc\.ankr\.com\/arbitrum\/[A-Za-z0-9]/i.test(RPC_HTTP)
    const hasQuery = /[?&](api_key|ankr_api_key)=/i.test(RPC_HTTP)
    const shouldSkipNakedAnkr = isAnkr && !hasKeyInPath && !hasQuery && keys.length > 0
    if (!shouldSkipNakedAnkr) {
      urls.unshift(RPC_HTTP)
    }
  }
  // Tek dahi olsa bir URL şart
  return urls.filter(Boolean)
}

const RPC_URLS = buildAnkrUrls()
if (!RPC_URLS.length) {
  console.error('No RPC URLs configured (set ARBITRUM_HTTP_RPC or ANKR_KEYS/ANKR_API_KEYS or ANKR_KEY_1.. or ANKR_API_KEY_1..)')
  process.exit(1)
}
const CLIENTS = RPC_URLS.map(u => createPublicClient({ chain: arbitrum, transport: viemHttp(u) }))
let CURRENT_IDX = 0

function isRateLimitError(err) {
  const msg = String(err && (err.message || err)).toLowerCase()
  return (
    msg.includes('rate') ||
    msg.includes('too many') ||
    msg.includes('429') ||
    msg.includes('limit') ||
    msg.includes('quota')
  )
}

function maskRpcUrl(u) {
  try {
    if (!u) return ''
    const url = new URL(u)
    // Path key mask: keep first 4 chars of last segment
    const parts = url.pathname.split('/').filter(Boolean)
    if (parts.length >= 2 && parts[0].toLowerCase() === 'arbitrum') {
      const last = parts[1] || ''
      if (last.length > 4) parts[1] = last.slice(0, 4) + '****'
      url.pathname = '/' + parts.join('/')
    }
    // Query key mask
    for (const k of ['api_key', 'ankr_api_key', 'key']) {
      if (url.searchParams.has(k)) {
        const v = url.searchParams.get(k) || ''
        url.searchParams.set(k, v.slice(0, 4) + '****')
      }
    }
    return url.toString()
  } catch {
    return String(u).replace(/([A-Za-z0-9]{4})[A-Za-z0-9]+$/, '$1****')
  }
}

function shortHash(s) {
  if (!s || typeof s !== 'string') return ''
  if (s.length <= 16) return s
  return s.slice(0, 10) + '…' + s.slice(-6)
}

function shortAddr(a) {
  if (!a || typeof a !== 'string') return ''
  if (a.length <= 12) return a
  return a.slice(0, 6) + '…' + a.slice(-4)
}

function hexToAddressMaybe(topic) {
  // topic like 0x000...<20-byte>
  if (typeof topic !== 'string' || !topic.startsWith('0x') || topic.length < 2 + 40) return ''
  const hex = topic.slice(2)
  if (hex.length < 40) return ''
  const last40 = hex.slice(-40)
  return '0x' + last40
}

function extractIndexedAddresses(topics) {
  const addrs = []
  if (!Array.isArray(topics)) return addrs
  for (let i = 1; i < topics.length; i++) {
    const a = hexToAddressMaybe(topics[i])
    if (a && /^0x[a-fA-F0-9]{40}$/.test(a)) addrs.push(a.toLowerCase())
  }
  return Array.from(new Set(addrs))
}

async function callWithFailover(fn) {
  const n = CLIENTS.length
  let lastErr
  for (let step = 0; step < n; step++) {
    const idx = (CURRENT_IDX + step) % n
    const cl = CLIENTS[idx]
    try {
      const res = await fn(cl, RPC_URLS[idx])
      // Başarılı, bu index ile devam
      CURRENT_IDX = idx
      return res
    } catch (e) {
      lastErr = e
      // Rate limit veya ağ hatası: sıradaki key'e geç
      if (isRateLimitError(e) || (e && e.name === 'FetchError')) {
        const nextIdx = (idx + 1) % n
        // eslint-disable-next-line no-console
        console.warn('[rpc-failover] switching due to rate-limit/error:', e?.message || e, 'from', idx, '->', nextIdx, 'url', maskRpcUrl(RPC_URLS[nextIdx]))
        CURRENT_IDX = nextIdx
        continue
      }
      // Diğer hatalarda da deneyip devam edelim
      continue
    }
  }
  throw lastErr || new Error('All RPCs failed')
}

const TRANSFER_EVENT = parseAbiItem('event Transfer(address indexed from, address indexed to, uint256 value)')
const MODULE_INSTALLED_EVENT = parseAbiItem('event ModuleInstalled(bytes32)')
const METHOD_INSTALL_MODULE = '0x7eb1e761'
const METHOD_ADDED_POOL = '0xb54c41e9'

let lastScannedBlock

async function getLatestConfirmed() {
  const latest = await callWithFailover(cl => cl.getBlockNumber())
  return latest - BigInt(CONFIRMATIONS)
}

async function ensureStart() {
  if (lastScannedBlock) return lastScannedBlock
  if (START_BLOCK === 'latest') {
    const target = await getLatestConfirmed()
    if (BACKFILL_BLOCKS > 0) {
      const bf = BigInt(BACKFILL_BLOCKS)
      lastScannedBlock = target > bf ? target - bf : 0n
    } else {
      lastScannedBlock = target
    }
  } else {
    lastScannedBlock = BigInt(START_BLOCK)
  }
  return lastScannedBlock
}

async function hydrateFromNotifierIfNeeded() {
  if (WATCH_ADDRESSES.length > 0) return
  const base = process.env.BACKEND_API_URL || 'http://localhost:8080'
  try {
    const res = await fetch(`${base}/config/watch`)
    if (res.ok) {
      const j = await res.json()
      if (j && Array.isArray(j.watchAddresses) && j.watchAddresses.length) {
        WATCH_ADDRESSES = j.watchAddresses
        console.log('loaded watch addresses from notifier profile=', j.profile, 'count=', WATCH_ADDRESSES.length)
      }
    }
  } catch {}
}

// Basit TTL'li native tx de-dupe
const seenNativeTx = new Map()
const SEEN_TTL_MS = Number(process.env.SEEN_TTL_MS || 10 * 60 * 1000) // 10dk varsayılan

// Native tarama periyodu ve blok sınırı
const NATIVE_POLL_MS = Number(process.env.NATIVE_POLL_MS || 15000)
const NATIVE_MAX_BLOCKS = Number(process.env.NATIVE_MAX_BLOCKS || 60)

// --- ABI yükleme: listener/abis altından adres->ABI haritası ---
const ADDRESS_TO_ABI = new Map()
function loadAbisFromDir(baseDir) {
  try {
    if (!fs.existsSync(baseDir)) return
    const files = fs.readdirSync(baseDir)
    for (const f of files) {
      const lower = f.toLowerCase()
      if (!lower.endsWith('.json') && !lower.endsWith('.abi')) continue
      const addrPart = f.split('.')[0]
      if (!/^0x[a-fA-F0-9]{40}$/.test(addrPart)) continue
      const full = path.join(baseDir, f)
      try {
        const raw = fs.readFileSync(full, 'utf8')
        const parsed = JSON.parse(raw)
        // Accept both ABI arrays and full JSON with abi field
        const abi = Array.isArray(parsed) ? parsed : (parsed.abi || [])
        if (Array.isArray(abi) && abi.length) {
          ADDRESS_TO_ABI.set(addrPart.toLowerCase(), abi)
        }
      } catch {}
    }
  } catch {}
}

function initLoadAbis() {
  // Try both repo-relative and container path
  const candidates = [
    path.join(process.cwd(), 'listener', 'abis'),
    '/app/listener/abis',
  ]
  for (const d of candidates) loadAbisFromDir(d)
  if (ADDRESS_TO_ABI.size) {
    // eslint-disable-next-line no-console
    console.log(`loaded ${ADDRESS_TO_ABI.size} abis for event decoding`)
  }
}

function decodeEventWithAbi(log) {
  const addr = String(log.address || '').toLowerCase()
  const abi = ADDRESS_TO_ABI.get(addr)
  if (!abi) return null
  try {
    const decoded = decodeEventLog({ abi, data: log.data, topics: log.topics })
    // Normalize args to simple JSON-friendly types
    const args = {}
    if (decoded && decoded.args) {
      for (const [k, v] of Object.entries(decoded.args)) {
        if (typeof v === 'bigint') args[k] = v.toString()
        else if (v && typeof v === 'object' && 'toString' in v) args[k] = String(v)
        else args[k] = v
      }
    }
    return { name: decoded.eventName || decoded.eventName === '' ? decoded.eventName : (decoded.eventName || 'Event'), args }
  } catch {
    return null
  }
}

// --- ERC20 sembol/decimal cache ---
const ERC20_ABI_MIN = [
  { type: 'function', name: 'symbol', stateMutability: 'view', inputs: [], outputs: [{ type: 'string' }] },
  { type: 'function', name: 'name', stateMutability: 'view', inputs: [], outputs: [{ type: 'string' }] },
  { type: 'function', name: 'decimals', stateMutability: 'view', inputs: [], outputs: [{ type: 'uint8' }] },
]
const TOKEN_META_CACHE = new Map() // addrLower -> { symbol, name, decimals, ts }

async function fetchTokenMeta(addr) {
  const key = String(addr || '').toLowerCase()
  if (!/^0x[a-f0-9]{40}$/.test(key)) return null
  const cached = TOKEN_META_CACHE.get(key)
  if (cached && Date.now() - cached.ts < 6 * 60 * 60 * 1000) return cached
  try {
    const [symbol, decimals, name] = await callWithFailover(async (cl) => {
      const [sym, dec, nm] = await Promise.all([
        cl.readContract({ address: key, abi: ERC20_ABI_MIN, functionName: 'symbol' }).catch(() => ''),
        cl.readContract({ address: key, abi: ERC20_ABI_MIN, functionName: 'decimals' }).catch(() => 18),
        cl.readContract({ address: key, abi: ERC20_ABI_MIN, functionName: 'name' }).catch(() => ''),
      ])
      return [sym || '', Number(dec || 18) || 18, nm || '']
    })
    const meta = { symbol: String(symbol || '').toUpperCase(), name: String(name || ''), decimals: Number.isFinite(decimals) ? decimals : 18, ts: Date.now() }
    TOKEN_META_CACHE.set(key, meta)
    return meta
  } catch {
    return null
  }
}

async function loop() {
  if (ADDRESS_TO_ABI.size === 0) initLoadAbis()
  await hydrateFromNotifierIfNeeded()
  // Guard: İzlenecek adres yoksa tarama yapma
  if (!Array.isArray(WATCH_ADDRESSES) || WATCH_ADDRESSES.length === 0) {
    // eslint-disable-next-line no-console
    console.warn('skip scan: WATCH_ADDRESSES is empty')
    return
  }
  try {
    await ensureStart()
    const target = await getLatestConfirmed()
    if (target <= lastScannedBlock) {
      return
    }

    const from = lastScannedBlock + 1n
    const to = from + BigInt(CHUNK_BLOCKS)
    const end = to > target ? target : to

    const filters = []
    const addrTopics = WATCH_ADDRESSES.map(a => a.toLowerCase())

    // token listli optimize: tüm token adreslerini tek çağrıda sorgula (to ve from için ayrı ayrı)
    if (TOKEN_LIST.length > 0) {
      filters.push({ address: TOKEN_LIST, event: TRANSFER_EVENT, args: { to: addrTopics } })
      filters.push({ address: TOKEN_LIST, event: TRANSFER_EVENT, args: { from: addrTopics } })
    } else {
      // genel Transfer filtresi: incoming ve outgoing
      filters.push({ event: TRANSFER_EVENT, args: { to: addrTopics } })
      filters.push({ event: TRANSFER_EVENT, args: { from: addrTopics } })
    }

    // ModuleInstalled eventleri (önemli): izlenen kontrat adresleri üzerinde filtrele
    if (WATCH_ADDRESSES.length > 0) {
      filters.push({ address: WATCH_ADDRESSES, event: MODULE_INSTALLED_EVENT })
    }

    // İsteğe bağlı: Tüm event'ler için adres bazlı genel filtre (ENV ile kontrol)
    if (ENABLE_GENERIC_EVENTS && WATCH_ADDRESSES.length > 0) {
      filters.push({ address: WATCH_ADDRESSES })
    }

    let found = 0
    // Basit deduplikasyon set'i (aynı log iki filtreye düşebilir)
    const seen = new Set()
    for (const f of filters) {
      const logs = await callWithFailover(cl => cl.getLogs({ ...f, fromBlock: from, toBlock: end }))
      for (const log of logs) {
        const key = `${log.blockNumber}:${log.logIndex}:${log.transactionHash}`
        if (seen.has(key)) continue
        seen.add(key)
        found++
        const decoded = decodeEventWithAbi(log)
        if (decoded && decoded.name && decoded.name !== 'Transfer') {
          await fetch(NOTIFIER_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              type: 'contract_event',
              title: decoded.name,
              addr: log.address,
              token: 'N/A',
              txHash: log.transactionHash,
              block: Number(log.blockNumber),
              timestamp: 0,
              meta: { args: decoded.args }
            })
          }).catch(() => {})
        } else if (log.eventName === 'ModuleInstalled') {
          await fetch(NOTIFIER_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              type: 'module',
              title: 'InstallModule',
              addr: log.address,
              token: 'N/A',
              txHash: log.transactionHash,
              block: Number(log.blockNumber),
              timestamp: 0,
              meta: { moduleHash: (log.args?.[0] || log.args?.module || '').toString?.() || '' }
            })
          }).catch(() => {})
        } else if (log.eventName === 'Transfer') {
          const toAddr = log.args.to || ''
          const fromAddr = log.args.from || ''
          const value = log.args.value?.toString?.() ?? ''
          const tokenAddr = log.address
          const metaExtra = await fetchTokenMeta(tokenAddr)
          const titleBase = fromAddr && addrTopics.includes(String(fromAddr).toLowerCase()) ? 'ERC20 OUT' : 'ERC20 IN'
          const title = metaExtra && metaExtra.symbol ? `${titleBase} • ${metaExtra.symbol}` : titleBase
          await fetch(NOTIFIER_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              type: 'erc20',
              title,
              addr: toAddr || fromAddr,
              token: tokenAddr || 'unknown',
              txHash: log.transactionHash,
              block: Number(log.blockNumber),
              timestamp: 0,
              meta: {
                value,
                tokenAddress: tokenAddr,
                tokenSymbol: metaExtra?.symbol || '',
                tokenName: metaExtra?.name || '',
                tokenDecimals: metaExtra?.decimals ?? null,
                from: String(fromAddr).toLowerCase(),
                to: String(toAddr).toLowerCase(),
              }
            })
          }).catch(() => {})
        }
      }
    }

    // Native ETH tarama: ayrı periyot kontrolü ile sınırlı sayıda blok taransın
    const nowMs = Date.now()
    if (!loop._lastNative || (nowMs - loop._lastNative) >= NATIVE_POLL_MS) {
      loop._lastNative = nowMs
      const wantBlocks = end - from + 1n
      const nativeScanCount = wantBlocks > BigInt(NATIVE_MAX_BLOCKS) ? NATIVE_MAX_BLOCKS : Number(wantBlocks)
      if (nativeScanCount > 0) {
        const addrs = new Set(addrTopics)
        const startNum = Number(end) - nativeScanCount + 1
        for (let b = startNum; b <= Number(end); b++) {
          const block = await callWithFailover(cl => cl.getBlock({ blockNumber: BigInt(b), includeTransactions: true }))
          const ts = Number(block.timestamp || 0n)
          for (const tx of block.transactions || []) {
            try {
              if (!tx) continue
              if (typeof tx.value !== 'bigint') continue
              const fromAddr = String(tx.from || '').toLowerCase()
              const toAddr = String(tx.to || '').toLowerCase()
              if (!addrs.has(fromAddr) && !addrs.has(toAddr)) continue
              const h = String(tx.hash)
              pruneSeen()
              if (seenNativeTx.has(h)) continue
              seenNativeTx.set(h, Date.now())
              const dir = addrs.has(fromAddr) && addrs.has(toAddr) ? 'internal' : (addrs.has(fromAddr) ? 'out' : 'in')
              const input = String(tx.input || '').toLowerCase()
              const isInstallModule = input.startsWith(METHOD_INSTALL_MODULE)
              const isAddPool = input.startsWith(METHOD_ADDED_POOL)
              if (tx.value > 0n) {
                await fetch(NOTIFIER_URL, {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({
                    type: 'native',
                    title: `NATIVE ETH ${dir.toUpperCase()}`,
                    addr: addrs.has(fromAddr) ? fromAddr : toAddr,
                    token: 'ETH',
                    txHash: h,
                    block: Number(block.number || b),
                    timestamp: ts,
                    meta: { valueWei: tx.value.toString(), from: fromAddr, to: toAddr, dir },
                  })
                }).catch(() => {})
              } else if (isInstallModule || isAddPool) {
                const title = isInstallModule ? 'InstallModule' : 'AddPool'
                await fetch(NOTIFIER_URL, {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({
                    type: 'module_call',
                    title,
                    addr: addrs.has(fromAddr) ? fromAddr : toAddr,
                    token: 'N/A',
                    txHash: h,
                    block: Number(block.number || b),
                    timestamp: ts,
                    meta: { methodId: input.slice(0,10), from: fromAddr, to: toAddr, dir },
                  })
                }).catch(() => {})
              }
            } catch {}
          }
        }
      }
    }

    lastScannedBlock = end
    // eslint-disable-next-line no-console
    console.log(JSON.stringify({ from: Number(from), to: Number(end), scanned_blocks: Number(end - from + 1n), found_logs: found }))
  } catch (e) {
    // eslint-disable-next-line no-console
    console.error('loop error', e?.message || e)
  }
}

setInterval(loop, POLL_MS)
loop()

// Basit healthcheck HTTP sunucusu (Render/Web Service için)
const HEALTH_PORT = process.env.PORT || 10000
const server = http.createServer((req, res) => {
  if (req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'text/plain' })
    res.end('ok')
  } else {
    res.writeHead(404, { 'Content-Type': 'text/plain' })
    res.end('not found')
  }
})
server.listen(HEALTH_PORT, () => {
  // eslint-disable-next-line no-console
  console.log(`health server listening on :${HEALTH_PORT}`)
})


