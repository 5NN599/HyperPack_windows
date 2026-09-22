#pragma once

#include <cstdint>
#include <functional>
#include <string>
#include <vector>

namespace hyperpack {

struct Options {
    int level = 9;
    int threads = 0;
    std::uint32_t group_size = 8u << 20;
    std::uint32_t dictionary = 96u << 20;
};

struct Progress {
    std::uint64_t done = 0;
    std::uint64_t total = 0;
    std::string stage;
};

using ProgressFn = std::function<void(const Progress&)>;

struct PackEntry {
    std::uint8_t kind = 0;
    std::string path;
    std::uint64_t offset = 0;
    std::uint64_t size = 0;
    std::uint32_t crc32 = 0;
};

Options options_for_level(int level, int threads = 0);

bool pack_files(const std::vector<std::string>& paths,
                const std::string& output,
                const Options& opt,
                const ProgressFn& progress,
                std::string& error);

bool pack_folder(const std::string& folder,
                 const std::string& output,
                 const Options& opt,
                 const ProgressFn& progress,
                 std::string& error);

bool extract_archive(const std::string& input,
                     const std::string& output,
                     const ProgressFn& progress,
                     std::string& error);

bool inspect_archive(const std::string& input,
                     std::vector<PackEntry>& entries,
                     std::uint64_t& original_size,
                     std::uint64_t& packed_size,
                     std::string& error);

} // namespace hyperpack
