#include "hyperpack.hpp"

#include <algorithm>
#include <array>
#include <atomic>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <future>
#include <queue>
#include <set>
#include <stdexcept>
#include <thread>
#include <vector>
#include <cstring>

#ifdef _WIN32
#  define NOMINMAX
#  include <windows.h>
#else
#  include <sys/sysinfo.h>
#  include <unistd.h>
#endif

namespace fs = std::filesystem;
namespace hyperpack {
namespace {

constexpr char MAGIC3[4] = {'H','P','K','3'};
constexpr std::uint8_t VERSION = 1;
constexpr std::uint8_t FLAG_PACKAGE = 1;
constexpr std::uint8_t GROUP_COMPRESSED = 0;
constexpr std::uint8_t GROUP_RAW = 1;
constexpr std::uint32_t MIN_MATCH = 4;
constexpr std::uint32_t MAX_MATCH = 1u << 20;
constexpr std::uint32_t HASH_BITS = 19;
constexpr std::uint32_t HASH_SIZE = 1u << HASH_BITS;
constexpr std::uint32_t MAX_SLOTS = 24;
constexpr std::uint16_t HUFF_ALPHABET = 256;

std::uint64_t physical_memory_bytes() {
#ifdef _WIN32
    MEMORYSTATUSEX s{};
    s.dwLength = sizeof(s);
    return GlobalMemoryStatusEx(&s) ? s.ullTotalPhys : (4ull << 30);
#else
    struct sysinfo s{};
    if (sysinfo(&s) == 0) return std::uint64_t(s.totalram) * s.mem_unit;
    const long pages = sysconf(_SC_PHYS_PAGES);
    const long page = sysconf(_SC_PAGE_SIZE);
    return pages > 0 && page > 0 ? std::uint64_t(pages) * std::uint64_t(page) : (4ull << 30);
#endif
}

void put_varint(std::vector<std::uint8_t>& out, std::uint64_t v) {
    while (v >= 0x80) {
        out.push_back(std::uint8_t(v) | 0x80u);
        v >>= 7;
    }
    out.push_back(std::uint8_t(v));
}

bool get_varint(const std::vector<std::uint8_t>& d, std::size_t& p, std::uint64_t& v) {
    v = 0;
    int shift = 0;
    while (p < d.size() && shift <= 63) {
        const std::uint8_t b = d[p++];
        v |= std::uint64_t(b & 0x7fu) << shift;
        if (!(b & 0x80u)) return true;
        shift += 7;
    }
    return false;
}

void write_varint(std::ofstream& out, std::uint64_t v) {
    while (v >= 0x80) {
        out.put(char((v & 0x7fu) | 0x80u));
        v >>= 7;
    }
    out.put(char(v));
}

bool read_varint(std::ifstream& in, std::uint64_t& v) {
    v = 0;
    int shift = 0;
    while (in && shift <= 63) {
        const int c = in.get();
        if (c == EOF) return false;
        const std::uint8_t b = std::uint8_t(c);
        v |= std::uint64_t(b & 0x7fu) << shift;
        if (!(b & 0x80u)) return true;
        shift += 7;
    }
    return false;
}

std::uint32_t crc32_update(std::uint32_t crc, const std::uint8_t* data, std::size_t n) {
    static const auto table = [] {
        std::array<std::uint32_t, 256> t{};
        for (std::uint32_t i = 0; i < 256; ++i) {
            std::uint32_t c = i;
            for (int j = 0; j < 8; ++j)
                c = (c & 1u) ? (0xedb88320u ^ (c >> 1)) : (c >> 1);
            t[i] = c;
        }
        return t;
    }();
    for (std::size_t i = 0; i < n; ++i) crc = table[(crc ^ data[i]) & 0xffu] ^ (crc >> 8);
    return crc;
}

std::uint32_t crc32(const std::uint8_t* data, std::size_t n) {
    return crc32_update(0xffffffffu, data, n) ^ 0xffffffffu;
}

std::uint32_t crc32_file(const fs::path& p, std::uint64_t& size) {
    std::ifstream in(p, std::ios::binary);
    if (!in) throw std::runtime_error("cannot open input file: " + p.string());
    std::vector<std::uint8_t> buf(1u << 20);
    std::uint32_t c = 0xffffffffu;
    size = 0;
    while (in) {
        in.read(reinterpret_cast<char*>(buf.data()), std::streamsize(buf.size()));
        const auto n = std::size_t(in.gcount());
        if (!n) break;
        c = crc32_update(c, buf.data(), n);
        size += n;
    }
    return c ^ 0xffffffffu;
}

// ---------------- Canonical Huffman ----------------
struct HuffCode {
    std::uint32_t code = 0;
    std::uint8_t bits = 0;
};

struct HuffmanTree {
    std::array<std::uint8_t, HUFF_ALPHABET> lengths{};
    std::array<HuffCode, HUFF_ALPHABET> codes{};
    std::vector<std::array<std::int32_t, 2>> child;
    std::vector<std::int32_t> symbol;

    static HuffmanTree build(const std::array<std::uint64_t, HUFF_ALPHABET>& freq) {
        struct Node { std::uint64_t f; int node; int sym; };
        struct Cmp { bool operator()(const Node& a, const Node& b) const { return a.f > b.f; } };
        std::priority_queue<Node, std::vector<Node>, Cmp> pq;
        std::vector<int> left, right, sym;
        std::vector<std::uint64_t> nf;

        for (int i = 0; i < HUFF_ALPHABET; ++i) {
            if (freq[std::size_t(i)] == 0) continue;
            const int id = int(nf.size());
            nf.push_back(freq[std::size_t(i)]);
            left.push_back(-1); right.push_back(-1); sym.push_back(i);
            pq.push({freq[std::size_t(i)], id, i});
        }
        if (pq.empty()) {
            const int id = int(nf.size());
            nf.push_back(1); left.push_back(-1); right.push_back(-1); sym.push_back(0);
            pq.push({1, id, 0});
        }
        if (pq.size() == 1) {
            HuffmanTree h;
            h.lengths[std::size_t(pq.top().sym)] = 1;
            h.make_canonical();
            h.make_decode_tree();
            return h;
        }

        while (pq.size() > 1) {
            const auto a = pq.top(); pq.pop();
            const auto b = pq.top(); pq.pop();
            const int id = int(nf.size());
            nf.push_back(a.f + b.f);
            left.push_back(a.node); right.push_back(b.node); sym.push_back(-1);
            pq.push({a.f + b.f, id, -1});
        }

        const int root = pq.top().node;
        std::array<std::uint8_t, HUFF_ALPHABET> lens{};
        std::function<void(int,int)> dfs = [&](int n, int depth) {
            if (sym[n] >= 0) {
                lens[std::size_t(sym[n])] = std::uint8_t(std::max(1, depth));
                return;
            }
            dfs(left[n], depth + 1);
            dfs(right[n], depth + 1);
        };
        dfs(root, 0);

        HuffmanTree h;
        h.lengths = lens;
        h.make_canonical();
        h.make_decode_tree();
        return h;
    }

    void make_canonical() {
        std::array<std::uint32_t, 33> count{};
        for (const auto l : lengths) if (l) {
            if (l > 32) throw std::runtime_error("Huffman code too deep");
            ++count[l];
        }
        std::array<std::uint32_t, 33> next{};
        std::uint32_t code = 0;
        for (int bits = 1; bits <= 32; ++bits) {
            code = (code + count[bits - 1]) << 1;
            next[bits] = code;
        }
        for (std::size_t s = 0; s < HUFF_ALPHABET; ++s) {
            const auto l = lengths[s];
            if (!l) continue;
            codes[s] = {next[l]++, l};
        }
    }

    void make_decode_tree() {
        child.clear();
        symbol.clear();
        child.push_back({-1, -1});
        symbol.push_back(-1);
        for (std::size_t s = 0; s < HUFF_ALPHABET; ++s) {
            const auto c = codes[s];
            if (!c.bits) continue;
            int node = 0;
            for (int i = int(c.bits) - 1; i >= 0; --i) {
                const int bit = int((c.code >> i) & 1u);
                if (child[node][bit] < 0) {
                    child[node][bit] = int(child.size());
                    child.push_back({-1, -1});
                    symbol.push_back(-1);
                }
                node = child[node][bit];
            }
            symbol[node] = int(s);
        }
    }

    bool valid() const { return !child.empty(); }
};

class BitWriter {
public:
    void bit(bool b) {
        cur_ = std::uint8_t((cur_ << 1) | (b ? 1u : 0u));
        ++bits_;
        if (bits_ == 8) {
            out_.push_back(cur_);
            cur_ = 0;
            bits_ = 0;
        }
    }

    void code(const HuffCode c) {
        for (int i = int(c.bits) - 1; i >= 0; --i) bit((c.code >> i) & 1u);
    }

    std::vector<std::uint8_t> finish() {
        if (bits_) out_.push_back(std::uint8_t(cur_ << (8 - bits_)));
        return std::move(out_);
    }

private:
    std::vector<std::uint8_t> out_;
    std::uint8_t cur_ = 0;
    int bits_ = 0;
};

class BitReader {
public:
    explicit BitReader(const std::vector<std::uint8_t>& in) : in_(in) {}
    bool bit() {
        if (pos_ >= in_.size() && bits_ == 0) throw std::runtime_error("bitstream underrun");
        if (!bits_) {
            cur_ = in_[pos_++];
            bits_ = 8;
        }
        const bool b = (cur_ & 0x80u) != 0;
        cur_ <<= 1;
        --bits_;
        return b;
    }
    std::uint16_t symbol(const HuffmanTree& h) {
        if (!h.valid()) throw std::runtime_error("invalid Huffman tree");
        int node = 0;
        for (;;) {
            if (h.symbol[std::size_t(node)] >= 0) return std::uint16_t(h.symbol[std::size_t(node)]);
            const int b = bit() ? 1 : 0;
            const int next = h.child[std::size_t(node)][b];
            if (next < 0) throw std::runtime_error("invalid Huffman code");
            node = next;
        }
    }
private:
    const std::vector<std::uint8_t>& in_;
    std::size_t pos_ = 0;
    std::uint8_t cur_ = 0;
    int bits_ = 0;
};

struct Token {
    std::uint32_t a = 0;
    std::uint32_t b = 0;
    // a high bit marks literal; otherwise a=distance-1 and b=length-MIN_MATCH.
    static Token literal(std::uint8_t c) { return {0x80000000u | c, 0}; }
    static Token match(std::uint32_t dist, std::uint32_t len) { return {dist - 1u, len - MIN_MATCH}; }
    bool is_literal() const { return (a & 0x80000000u) != 0; }
    std::uint8_t literal_byte() const { return std::uint8_t(a & 0xffu); }
    std::uint32_t dist() const { return (a & 0x7fffffffu) + 1u; }
    std::uint32_t len() const { return b + MIN_MATCH; }
};

std::uint32_t hash8(const std::uint8_t* p) {
    std::uint64_t x = 0;
    std::memcpy(&x, p, sizeof(x));
    x ^= x >> 33;
    x *= 0xff51afd7ed558ccduLL;
    x ^= x >> 33;
    x *= 0xc4ceb9fe1a85ec53uLL;
    x ^= x >> 33;
    return std::uint32_t(x) & (HASH_SIZE - 1u);
}

class MatchFinder {
public:
    MatchFinder(const std::uint8_t* data, std::size_t size, std::size_t body_start,
                std::uint32_t dict, int level)
        : data_(data), size_(size), body_start_(body_start), dict_(dict),
          slots_(std::size_t(HASH_SIZE) * MAX_SLOTS, 0xffffffffu),
          next_(HASH_SIZE, 0), limit_(level >= 9 ? 24u : level >= 7 ? 20u : level >= 5 ? 12u : 8u) {}

    void seed_prefix() {
        const std::size_t step = body_start_ > (32u << 20) ? 2 : 1;
        for (std::size_t p = 0; p + 8 <= body_start_; p += step) insert(p);
    }

    void insert(std::size_t pos) {
        if (pos + 8 > size_) return;
        const auto h = hash8(data_ + pos);
        auto* row = slots_.data() + std::size_t(h) * MAX_SLOTS;
        auto& n = next_[h];
        row[n] = std::uint32_t(pos);
        n = std::uint8_t((n + 1) % MAX_SLOTS);
    }

    std::pair<std::uint32_t, std::uint32_t> find(std::size_t pos) const {
        if (pos + 8 > size_) return {0, 0};
        const auto h = hash8(data_ + pos);
        const auto* row = slots_.data() + std::size_t(h) * MAX_SLOTS;
        std::uint32_t best_len = 0, best_dist = 0, seen = 0;
        for (std::uint32_t i = 0; i < MAX_SLOTS && seen < limit_; ++i) {
            const auto cand = row[i];
            if (cand == 0xffffffffu || cand >= pos) continue;
            const auto dist = std::uint32_t(pos - cand);
            if (dist == 0 || dist > dict_) continue;
            ++seen;
            const auto max_len = std::min<std::size_t>(MAX_MATCH, size_ - pos);
            std::size_t len = 0;
            while (len < max_len && data_[cand + len] == data_[pos + len]) ++len;
            if (len >= MIN_MATCH && len > best_len) {
                best_len = std::uint32_t(len);
                best_dist = dist;
                if (len == max_len) break;
            }
        }
        return {best_len, best_dist};
    }

private:
    const std::uint8_t* data_;
    std::size_t size_;
    std::size_t body_start_;
    std::uint32_t dict_;
    std::vector<std::uint32_t> slots_;
    std::vector<std::uint8_t> next_;
    std::uint32_t limit_;
};

std::uint32_t token_cost(std::uint32_t len, std::uint32_t dist) {
    if (!len) return 9;
    auto vb = [](std::uint32_t x) {
        std::uint32_t n = 1;
        while (x >= 0x80u) { x >>= 7; ++n; }
        return n;
    };
    return 1 + 8 * (vb(dist - 1) + vb(len - MIN_MATCH));
}

void count_meta(std::array<std::uint64_t, 256>& f, std::uint64_t v) {
    while (v >= 0x80) {
        ++f[std::size_t((v & 0x7fu) | 0x80u)];
        v >>= 7;
    }
    ++f[std::size_t(v)];
}

void write_meta(BitWriter& bw, const HuffmanTree& h, std::uint64_t v) {
    while (v >= 0x80) {
        bw.code(h.codes[std::size_t((v & 0x7fu) | 0x80u)]);
        v >>= 7;
    }
    bw.code(h.codes[std::size_t(v)]);
}

std::uint64_t read_meta(BitReader& br, const HuffmanTree& h) {
    std::uint64_t v = 0;
    int shift = 0;
    for (;;) {
        const auto b = std::uint8_t(br.symbol(h));
        v |= std::uint64_t(b & 0x7fu) << shift;
        if (!(b & 0x80u)) return v;
        shift += 7;
        if (shift >= 64) throw std::runtime_error("meta varint overflow");
    }
}

struct GroupResult {
    std::uint32_t raw_size = 0;
    std::uint32_t crc = 0;
    std::uint8_t mode = GROUP_RAW;
    std::vector<std::uint8_t> payload;
};

GroupResult encode_group(const std::uint8_t* data, std::size_t size, std::size_t body_start,
                         int level, std::uint32_t dict) {
    GroupResult result;
    result.raw_size = std::uint32_t(size - body_start);
    result.crc = crc32(data + body_start, size - body_start);
    if (!result.raw_size) return result;
    if (level <= 0) {
        result.mode = GROUP_RAW;
        result.payload.assign(data + body_start, data + size);
        return result;
    }

    MatchFinder mf(data, size, body_start, dict, level);
    mf.seed_prefix();

    std::vector<Token> tokens;
    tokens.reserve(result.raw_size / 2 + 8);
    std::array<std::uint64_t, 256> literal_freq{};
    std::array<std::uint64_t, 256> meta_freq{};

    std::size_t pos = body_start;
    while (pos < size) {
        auto best = mf.find(pos);
        std::uint32_t len = best.first;
        std::uint32_t dist = best.second;

        if (level >= 6 && len >= MIN_MATCH) {
            const auto current_cost = token_cost(len, dist);
            for (int look = 1; look <= 3 && pos + std::size_t(look) < size; ++look) {
                const auto nxt = mf.find(pos + std::size_t(look));
                if (nxt.first >= MIN_MATCH && std::uint32_t(look) * 9u + token_cost(nxt.first, nxt.second) + 3u < current_cost) {
                    len = 0;
                    dist = 0;
                    break;
                }
            }
        }

        if (len >= MIN_MATCH) {
            tokens.push_back(Token::match(dist, len));
            count_meta(meta_freq, std::uint64_t(dist - 1));
            count_meta(meta_freq, std::uint64_t(len - MIN_MATCH));
            for (std::size_t q = 0; q < len; ++q) mf.insert(pos + q);
            pos += len;
        } else {
            const auto c = data[pos];
            tokens.push_back(Token::literal(c));
            ++literal_freq[c];
            mf.insert(pos);
            ++pos;
        }
    }

    const auto lit = HuffmanTree::build(literal_freq);
    const auto meta = HuffmanTree::build(meta_freq);
    BitWriter bw;
    for (const auto& t : tokens) {
        if (t.is_literal()) {
            bw.bit(false);
            bw.code(lit.codes[t.literal_byte()]);
        } else {
            bw.bit(true);
            write_meta(bw, meta, t.dist() - 1);
            write_meta(bw, meta, t.len() - MIN_MATCH);
        }
    }
    auto bitstream = bw.finish();

    // A compressed group stores the two canonical length tables (512 bytes)
    // followed by the bitstream. If that is not smaller than raw, store raw.
    const std::size_t table_bytes = 512;
    if (bitstream.size() + table_bytes >= result.raw_size) {
        result.mode = GROUP_RAW;
        result.payload.assign(data + body_start, data + size);
        return result;
    }

    result.mode = GROUP_COMPRESSED;
    result.payload.reserve(table_bytes + bitstream.size());
    result.payload.insert(result.payload.end(), lit.lengths.begin(), lit.lengths.end());
    result.payload.insert(result.payload.end(), meta.lengths.begin(), meta.lengths.end());
    result.payload.insert(result.payload.end(), bitstream.begin(), bitstream.end());
    return result;
}

HuffmanTree tree_from_lengths(const std::vector<std::uint8_t>& lengths, std::size_t off) {
    HuffmanTree h;
    for (std::size_t i = 0; i < HUFF_ALPHABET; ++i) h.lengths[i] = lengths[off + i];
    h.make_canonical();
    h.make_decode_tree();
    return h;
}

bool decode_group_hpk3(const std::vector<std::uint8_t>& enc, std::size_t expected,
                       std::vector<std::uint8_t>& prefix, std::uint32_t dict,
                       std::uint8_t mode, std::vector<std::uint8_t>& out,
                       std::string& error) {
    try {
        out.clear();
        out.reserve(expected);
        if (mode == GROUP_RAW) {
            if (enc.size() != expected) { error = "raw group size mismatch"; return false; }
            out = enc;
        } else {
            if (enc.size() < 512) { error = "compressed group too small"; return false; }
            const auto lit = tree_from_lengths(enc, 0);
            const auto meta = tree_from_lengths(enc, 256);
            std::vector<std::uint8_t> bits(enc.begin() + 512, enc.end());
            BitReader br(bits);
            while (out.size() < expected) {
                const bool is_match = br.bit();
                if (!is_match) {
                    const auto b = std::uint8_t(br.symbol(lit));
                    out.push_back(b);
                } else {
                    const auto dist = read_meta(br, meta) + 1;
                    const auto len = read_meta(br, meta) + MIN_MATCH;
                    if (dist == 0 || dist > dict || dist > prefix.size() + out.size() || len > expected - out.size()) {
                        error = "invalid match";
                        return false;
                    }
                    for (std::uint64_t i = 0; i < len; ++i) {
                        const auto total_pos = prefix.size() + out.size();
                        const auto src = total_pos - std::size_t(dist);
                        out.push_back(src < prefix.size() ? prefix[src] : out[src - prefix.size()]);
                    }
                }
            }
        }
        prefix.insert(prefix.end(), out.begin(), out.end());
        if (prefix.size() > dict) prefix.erase(prefix.begin(), prefix.end() - dict);
        return true;
    } catch (const std::exception& e) {
        error = e.what();
        return false;
    }
}

bool decode_group_hpk2(const std::vector<std::uint8_t>& enc, std::size_t expected,
                       std::vector<std::uint8_t>& prefix, std::uint32_t dict,
                       std::vector<std::uint8_t>& out, std::string& error) {
    out.clear();
    out.reserve(expected);
    std::size_t p = 0;
    while (out.size() < expected) {
        if (p >= enc.size()) { error = "truncated control byte"; return false; }
        const auto control = enc[p++];
        for (int bit = 0; bit < 8 && out.size() < expected; ++bit) {
            if ((control & (1u << bit)) == 0) {
                if (p >= enc.size()) { error = "truncated literal"; return false; }
                out.push_back(enc[p++]);
            } else {
                std::uint64_t dist = 0, lenm = 0;
                if (!get_varint(enc, p, dist) || !get_varint(enc, p, lenm)) { error = "truncated match"; return false; }
                const auto len = lenm + MIN_MATCH;
                if (!dist || dist > dict || dist > prefix.size() + out.size() || len > expected - out.size()) { error = "invalid match"; return false; }
                for (std::uint64_t i = 0; i < len; ++i) {
                    const auto total_pos = prefix.size() + out.size();
                    const auto src = total_pos - std::size_t(dist);
                    out.push_back(src < prefix.size() ? prefix[src] : out[src - prefix.size()]);
                }
            }
        }
    }
    if (p != enc.size()) { error = "trailing encoded bytes"; return false; }
    prefix.insert(prefix.end(), out.begin(), out.end());
    if (prefix.size() > dict) prefix.erase(prefix.begin(), prefix.end() - dict);
    return true;
}

std::uint64_t total_input_size(const std::vector<PackEntry>& entries) {
    std::uint64_t n = 0;
    for (const auto& e : entries) if (!e.kind) n += e.size;
    return n;
}

std::string normalize(const fs::path& p) {
    auto s = p.generic_string();
    while (s.rfind("./", 0) == 0) s.erase(0, 2);
    return s;
}

bool safe_path(const std::string& s) {
    if (s.empty() || s[0] == '/' || s.find(':') != std::string::npos) return false;
    std::size_t i = 0;
    while (i < s.size()) {
        auto j = s.find('/', i);
        if (j == std::string::npos) j = s.size();
        if (s.substr(i, j - i) == "..") return false;
        i = j + 1;
    }
    return true;
}

void collect_folder(const fs::path& root, std::vector<PackEntry>& entries) {
    std::vector<fs::path> paths{root};
    for (const auto& x : fs::recursive_directory_iterator(root)) paths.push_back(x.path());
    std::sort(paths.begin(), paths.end());
    for (const auto& p : paths) {
        const auto rp = normalize(fs::relative(p, root.parent_path()));
        if (!safe_path(rp)) throw std::runtime_error("unsafe archive path: " + rp);
        PackEntry e;
        e.kind = fs::is_directory(p) ? 1 : 0;
        e.path = rp;
        if (!e.kind) e.crc32 = crc32_file(p, e.size);
        entries.push_back(std::move(e));
    }
}

fs::path common_root(const std::vector<std::string>& paths) {
    if (paths.empty()) return fs::current_path();
    fs::path root = fs::absolute(paths.front()).parent_path();
    for (std::size_t i = 1; i < paths.size(); ++i) {
        fs::path d = fs::absolute(paths[i]).parent_path();
        while (true) {
            const auto rs = root.generic_string();
            const auto ds = d.generic_string();
            if (rs == ds || (rs.size() > ds.size() && rs.rfind(ds + "/", 0) == 0)) break;
            const auto parent = d.parent_path();
            if (parent == d) break;
            d = parent;
        }
        root = d;
    }
    return root.empty() ? fs::current_path() : root;
}

std::vector<PackEntry> collect_files(const std::vector<std::string>& paths) {
    if (paths.empty()) throw std::runtime_error("no files selected");
    const auto root = common_root(paths);
    std::vector<PackEntry> entries;
    std::set<std::string> seen;
    for (const auto& sp : paths) {
        const auto p = fs::absolute(sp);
        if (!fs::is_regular_file(p)) continue;
        const auto rp = normalize(fs::relative(p, root));
        if (!safe_path(rp)) throw std::runtime_error("unsafe archive path: " + rp);
        if (!seen.insert(rp).second) throw std::runtime_error("duplicate archive path: " + rp);
        PackEntry e;
        e.path = rp;
        e.crc32 = crc32_file(p, e.size);
        entries.push_back(std::move(e));
    }
    return entries;
}

void write_u32(std::ofstream& out, std::uint32_t v) {
    char b[4] = {char(v), char(v >> 8), char(v >> 16), char(v >> 24)};
    out.write(b, 4);
}
void write_u64(std::ofstream& out, std::uint64_t v) {
    char b[8];
    for (int i = 0; i < 8; ++i) b[i] = char(v >> (8 * i));
    out.write(b, 8);
}
bool read_u32(std::ifstream& in, std::uint32_t& v) {
    char b[4]; in.read(b, 4); if (in.gcount() != 4) return false;
    v = std::uint8_t(b[0]) | (std::uint32_t(std::uint8_t(b[1])) << 8) |
        (std::uint32_t(std::uint8_t(b[2])) << 16) | (std::uint32_t(std::uint8_t(b[3])) << 24);
    return true;
}
bool read_u64(std::ifstream& in, std::uint64_t& v) {
    char b[8]; in.read(b, 8); if (in.gcount() != 8) return false;
    v = 0; for (int i = 0; i < 8; ++i) v |= std::uint64_t(std::uint8_t(b[i])) << (8 * i);
    return true;
}

struct Header {
    std::uint8_t flags = 0;
    std::uint32_t group = 0, dict = 0, groups = 0, entries = 0;
    std::uint64_t manifest = 0, original = 0;
    std::uint32_t crc = 0;
};

void write_header(std::ofstream& out, const Header& h) {
    out.write(MAGIC3, 4); out.put(char(VERSION)); out.put(char(h.flags)); out.put(0); out.put(0);
    write_u32(out, h.group); write_u32(out, h.dict); write_u32(out, h.groups); write_u32(out, h.entries);
    write_u64(out, h.manifest); write_u64(out, h.original); write_u32(out, h.crc);
}

bool read_header(std::ifstream& in, Header& h, bool& is_hpk3) {
    char magic[4]; in.read(magic, 4); if (in.gcount() != 4) return false;
    const std::string m(magic, 4); if (m != "HPK3" && m != "HPK2") return false;
    is_hpk3 = m == "HPK3";
    const int ver = in.get(); if (ver != VERSION) return false;
    h.flags = std::uint8_t(in.get()); in.get(); in.get();
    return read_u32(in, h.group) && read_u32(in, h.dict) && read_u32(in, h.groups) &&
           read_u32(in, h.entries) && read_u64(in, h.manifest) && read_u64(in, h.original) && read_u32(in, h.crc);
}

bool read_manifest(std::ifstream& in, const Header& h, std::vector<PackEntry>& entries) {
    std::vector<std::uint8_t> m(std::size_t(h.manifest));
    if (!m.empty()) { in.read(reinterpret_cast<char*>(m.data()), std::streamsize(m.size())); if (std::size_t(in.gcount()) != m.size()) return false; }
    std::size_t p = 0; std::uint64_t count = 0;
    if (!get_varint(m, p, count) || count > 1'000'000) return false;
    entries.clear(); entries.reserve(std::size_t(count));
    for (std::uint64_t i = 0; i < count; ++i) {
        if (p >= m.size()) return false;
        PackEntry e; e.kind = m[p++]; std::uint64_t len = 0;
        if (!get_varint(m, p, len) || p + len > m.size()) return false;
        e.path.assign(reinterpret_cast<const char*>(m.data() + p), std::size_t(len)); p += std::size_t(len);
        if (!get_varint(m, p, e.offset) || !get_varint(m, p, e.size) || p + 4 > m.size()) return false;
        e.crc32 = std::uint32_t(m[p]) | (std::uint32_t(m[p+1]) << 8) |
                  (std::uint32_t(m[p+2]) << 16) | (std::uint32_t(m[p+3]) << 24); p += 4;
        entries.push_back(std::move(e));
    }
    return true;
}

fs::path build_files_temp(const std::vector<std::string>& paths, const std::vector<PackEntry>& entries,
                          const fs::path& tempdir) {
    const auto tmp = tempdir / "hyperpack-solid.tmp";
    std::ofstream out(tmp, std::ios::binary);
    if (!out) throw std::runtime_error("cannot create temp stream");
    const auto root = common_root(paths);
    std::vector<char> buf(1u << 20);
    for (const auto& e : entries) if (!e.kind) {
        const auto src = root / e.path;
        std::ifstream in(src, std::ios::binary);
        if (!in) throw std::runtime_error("cannot open " + src.string());
        while (in) {
            in.read(buf.data(), std::streamsize(buf.size()));
            const auto n = std::size_t(in.gcount());
            if (n) out.write(buf.data(), std::streamsize(n));
        }
        if (!out) throw std::runtime_error("failed writing solid stream");
    }
    out.close();
    return tmp;
}

fs::path build_folder_temp(const fs::path& root, const std::vector<PackEntry>& entries,
                           const fs::path& tempdir) {
    const auto tmp = tempdir / "hyperpack-solid.tmp";
    std::ofstream out(tmp, std::ios::binary);
    if (!out) throw std::runtime_error("cannot create temp stream");
    std::vector<char> buf(1u << 20);
    for (const auto& e : entries) if (!e.kind) {
        const auto src = root.parent_path() / e.path;
        std::ifstream in(src, std::ios::binary);
        if (!in) throw std::runtime_error("cannot open " + src.string());
        while (in) {
            in.read(buf.data(), std::streamsize(buf.size()));
            const auto n = std::size_t(in.gcount());
            if (n) out.write(buf.data(), std::streamsize(n));
        }
        if (!out) throw std::runtime_error("failed writing solid stream");
    }
    out.close();
    return tmp;
}

std::uint32_t crc32_stream(const fs::path& p) {
    std::ifstream in(p, std::ios::binary);
    if (!in) throw std::runtime_error("cannot read solid stream");
    std::vector<std::uint8_t> b(1u << 20);
    std::uint32_t c = 0xffffffffu;
    while (in) {
        in.read(reinterpret_cast<char*>(b.data()), std::streamsize(b.size()));
        const auto n = std::size_t(in.gcount());
        if (n) c = crc32_update(c, b.data(), n);
    }
    return c ^ 0xffffffffu;
}

std::vector<std::uint8_t> read_range(const fs::path& p, std::uint64_t off, std::size_t n) {
    std::ifstream in(p, std::ios::binary);
    if (!in) throw std::runtime_error("cannot open source: " + p.string());
    in.seekg(std::streamoff(off));
    std::vector<std::uint8_t> b(n);
    if (n) {
        in.read(reinterpret_cast<char*>(b.data()), std::streamsize(n));
        if (std::size_t(in.gcount()) != n) throw std::runtime_error("source changed while reading: " + p.string());
    }
    return b;
}

std::uint32_t choose_workers(const Options& opt, std::uint64_t groups) {
    int hw = opt.threads > 0 ? opt.threads : int(std::thread::hardware_concurrency());
    if (hw < 1) hw = 1;
    hw = std::min(hw, int(groups));
    const std::uint64_t per = std::uint64_t(opt.group_size) + std::uint64_t(opt.dictionary) +
                               std::uint64_t(HASH_SIZE) * MAX_SLOTS * sizeof(std::uint32_t) +
                               (64ull << 20);
    const auto budget = std::max<std::uint64_t>(1ull << 30, physical_memory_bytes() * 3 / 5);
    const auto by_mem = std::max<std::uint64_t>(1, budget / std::max<std::uint64_t>(1, per));
    return std::uint32_t(std::max<std::uint64_t>(1, std::min<std::uint64_t>(hw, by_mem)));
}

bool pack_from_entries(const std::vector<PackEntry>& entries, const fs::path& solid,
                       const std::string& output, const Options& opt,
                       const ProgressFn& progress, std::string& error) {
    try {
        const auto total = total_input_size(entries);
        const std::uint64_t group_size = opt.group_size;
        const std::uint32_t groups = std::max<std::uint32_t>(1, std::uint32_t((total + group_size - 1) / group_size));

        std::vector<std::uint8_t> manifest;
        put_varint(manifest, entries.size());
        for (const auto& e : entries) {
            manifest.push_back(e.kind); put_varint(manifest, e.path.size());
            manifest.insert(manifest.end(), e.path.begin(), e.path.end());
            put_varint(manifest, e.offset); put_varint(manifest, e.size);
            manifest.push_back(std::uint8_t(e.crc32)); manifest.push_back(std::uint8_t(e.crc32 >> 8));
            manifest.push_back(std::uint8_t(e.crc32 >> 16)); manifest.push_back(std::uint8_t(e.crc32 >> 24));
        }

        std::ofstream out(output, std::ios::binary);
        if (!out) throw std::runtime_error("cannot create output: " + output);
        const Header hdr{FLAG_PACKAGE, std::uint32_t(group_size), opt.dictionary, groups,
                         std::uint32_t(entries.size()), manifest.size(), total, crc32_stream(solid)};
        write_header(out, hdr);
        out.write(reinterpret_cast<const char*>(manifest.data()), std::streamsize(manifest.size()));

        const auto workers = choose_workers(opt, groups);
        std::atomic<std::uint32_t> next{0};
        std::atomic<std::uint64_t> done{0};
        std::vector<GroupResult> results(groups);
        std::vector<std::future<void>> jobs;
        jobs.reserve(workers);

        for (std::uint32_t t = 0; t < workers; ++t) {
            jobs.emplace_back(std::async(std::launch::async, [&] {
                while (true) {
                    const auto gi = next.fetch_add(1);
                    if (gi >= groups) break;
                    const std::uint64_t off = std::uint64_t(gi) * group_size;
                    const auto raw = std::uint32_t(std::min<std::uint64_t>(group_size, total - off));
                    const auto pre_start = off > opt.dictionary ? off - opt.dictionary : 0;
                    const auto prefix = std::size_t(off - pre_start);
                    const auto data = read_range(solid, pre_start, prefix + raw);
                    results[gi] = encode_group(data.data(), data.size(), prefix, opt.level, opt.dictionary);
                    const auto d = done.fetch_add(raw) + raw;
                    if (progress) progress({std::min(d, total), total, "HPK3 고압축 엔진"});
                }
            }));
        }
        for (auto& job : jobs) job.get();

        for (const auto& r : results) {
            write_varint(out, r.raw_size);
            if (r.mode == GROUP_RAW) {
                write_varint(out, r.payload.size());
                out.put(char(GROUP_RAW));
                write_u32(out, r.crc);
                out.write(reinterpret_cast<const char*>(r.payload.data()), std::streamsize(r.payload.size()));
            } else {
                write_varint(out, r.payload.size());
                out.put(char(GROUP_COMPRESSED));
                write_u32(out, r.crc);
                out.write(reinterpret_cast<const char*>(r.payload.data()), std::streamsize(r.payload.size()));
            }
        }
        if (!out) throw std::runtime_error("failed writing archive");
        if (progress) progress({total, total, "압축 완료"});
        return true;
    } catch (const std::exception& e) {
        error = e.what();
        return false;
    }
}

void fill_offsets(std::vector<PackEntry>& entries) {
    std::uint64_t off = 0;
    for (auto& e : entries) if (!e.kind) { e.offset = off; off += e.size; }
}

bool write_restored_files(const fs::path& solid, const fs::path& root,
                          const std::vector<PackEntry>& entries) {
    std::vector<std::uint8_t> buf(1u << 20);
    for (const auto& e : entries) {
        const auto dst = root / e.path;
        if (e.kind) { fs::create_directories(dst); continue; }
        std::ifstream rf(solid, std::ios::binary);
        if (!rf) return false;
        rf.seekg(std::streamoff(e.offset));
        fs::create_directories(dst.parent_path());
        std::ofstream wf(dst, std::ios::binary);
        if (!wf) return false;
        std::uint64_t left = e.size;
        std::uint32_t c = 0xffffffffu;
        while (left) {
            const auto want = std::size_t(std::min<std::uint64_t>(left, buf.size()));
            rf.read(reinterpret_cast<char*>(buf.data()), std::streamsize(want));
            if (std::size_t(rf.gcount()) != want) return false;
            wf.write(reinterpret_cast<const char*>(buf.data()), std::streamsize(want));
            c = crc32_update(c, buf.data(), want);
            left -= want;
        }
        if ((c ^ 0xffffffffu) != e.crc32) return false;
    }
    return true;
}

} // namespace

Options options_for_level(int level, int threads) {
    level = std::clamp(level, 0, 9);
    Options o;
    o.level = level;
    o.threads = threads > 0 ? threads : 0;
    if (level >= 9) { o.group_size = 8u << 20; o.dictionary = 96u << 20; }
    else if (level >= 7) { o.group_size = 8u << 20; o.dictionary = 64u << 20; }
    else if (level >= 5) { o.group_size = 8u << 20; o.dictionary = 32u << 20; }
    else { o.group_size = 4u << 20; o.dictionary = 16u << 20; }
    return o;
}

bool pack_files(const std::vector<std::string>& paths, const std::string& output,
                const Options& opt, const ProgressFn& progress, std::string& error) {
    try {
        auto entries = collect_files(paths);
        if (progress) progress({0, entries.size(), "메타데이터"});
        fill_offsets(entries);
        auto tempdir = fs::path(output).parent_path(); if (tempdir.empty()) tempdir = fs::current_path();
        const auto solid = build_files_temp(paths, entries, tempdir);
        if (progress) progress({entries.size(), entries.size(), "솔리드 스트림 준비 완료"});
        const bool ok = pack_from_entries(entries, solid, output, opt, progress, error);
        std::error_code ec; fs::remove(solid, ec);
        return ok;
    } catch (const std::exception& e) { error = e.what(); return false; }
}

bool pack_folder(const std::string& folder, const std::string& output,
                 const Options& opt, const ProgressFn& progress, std::string& error) {
    try {
        const fs::path root(folder);
        std::vector<PackEntry> entries;
        collect_folder(root, entries);
        if (progress) progress({entries.size(), entries.size(), "메타데이터"});
        fill_offsets(entries);
        auto tempdir = fs::path(output).parent_path(); if (tempdir.empty()) tempdir = fs::current_path();
        const auto solid = build_folder_temp(root, entries, tempdir);
        const bool ok = pack_from_entries(entries, solid, output, opt, progress, error);
        std::error_code ec; fs::remove(solid, ec);
        return ok;
    } catch (const std::exception& e) { error = e.what(); return false; }
}

bool inspect_archive(const std::string& input, std::vector<PackEntry>& entries,
                     std::uint64_t& original_size, std::uint64_t& packed_size,
                     std::string& error) {
    try {
        std::ifstream in(input, std::ios::binary);
        if (!in) throw std::runtime_error("cannot open archive");
        Header h{}; bool is_hpk3 = false;
        if (!read_header(in, h, is_hpk3)) throw std::runtime_error("invalid archive");
        if (!read_manifest(in, h, entries)) throw std::runtime_error("invalid manifest");
        original_size = h.original;
        in.seekg(0, std::ios::end); packed_size = std::uint64_t(in.tellg());
        return true;
    } catch (const std::exception& e) { error = e.what(); return false; }
}

bool extract_archive(const std::string& input, const std::string& output,
                     const ProgressFn& progress, std::string& error) {
    try {
        std::ifstream in(input, std::ios::binary);
        if (!in) throw std::runtime_error("cannot open archive");
        Header h{}; bool is_hpk3 = false;
        if (!read_header(in, h, is_hpk3)) throw std::runtime_error("invalid archive");
        std::vector<PackEntry> entries;
        if (!read_manifest(in, h, entries)) throw std::runtime_error("invalid manifest");
        for (const auto& e : entries) if (!safe_path(e.path)) throw std::runtime_error("unsafe path: " + e.path);

        const fs::path root(output); fs::create_directories(root);
        const auto solid = root / ".hyperpack-restore.tmp";
        std::ofstream raw(solid, std::ios::binary);
        if (!raw) throw std::runtime_error("cannot create restore temp");
        std::vector<std::uint8_t> prefix;
        std::uint64_t done = 0;

        for (std::uint32_t gi = 0; gi < h.groups; ++gi) {
            std::uint64_t unc = 0, comp = 0;
            if (!read_varint(in, unc) || !read_varint(in, comp)) throw std::runtime_error("truncated group header");
            int mode = GROUP_COMPRESSED;
            if (is_hpk3) { mode = in.get(); if (mode == EOF) throw std::runtime_error("truncated group mode"); }
            std::uint32_t gcrc = 0; if (!read_u32(in, gcrc)) throw std::runtime_error("truncated group crc");
            if (comp > (1ull << 30)) throw std::runtime_error("group too large");
            std::vector<std::uint8_t> enc(static_cast<std::size_t>(comp));
            if (comp) {
                in.read(reinterpret_cast<char*>(enc.data()), std::streamsize(enc.size()));
                if (std::size_t(in.gcount()) != enc.size()) throw std::runtime_error("truncated group");
            }
            std::vector<std::uint8_t> dec;
            std::string de;
            bool ok = false;
            if (is_hpk3) ok = decode_group_hpk3(enc, std::size_t(unc), prefix, h.dict, std::uint8_t(mode), dec, de);
            else ok = decode_group_hpk2(enc, std::size_t(unc), prefix, h.dict, dec, de);
            if (!ok) throw std::runtime_error("group decode: " + de);
            if (crc32(dec.data(), dec.size()) != gcrc) throw std::runtime_error("group CRC mismatch");
            if (!dec.empty()) raw.write(reinterpret_cast<const char*>(dec.data()), std::streamsize(dec.size()));
            done += dec.size();
            if (progress) progress({done, h.original, is_hpk3 ? "HPK3 복원" : "HPK2 복원"});
        }
        raw.close();
        if (!write_restored_files(solid, root, entries)) throw std::runtime_error("failed to restore files");
        std::error_code ec; fs::remove(solid, ec);
        if (progress) progress({h.original, h.original, "압축 해제 완료"});
        return true;
    } catch (const std::exception& e) { error = e.what(); return false; }
}

} // namespace hyperpack
