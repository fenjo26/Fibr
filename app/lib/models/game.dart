/// Game catalog model — one entry of `GET /api/games`
/// (docs/dns-games.md §3) plus an offline snapshot used when the server
/// is unreachable.
library;

/// One game of the catalog: id + bilingual name + processes + how many subnets its
/// routes span.
class GameInfo {
  const GameInfo({
    required this.id,
    required this.name,
    required this.nameRu,
    required this.cidrCount,
    this.processes = const [],
  });

  final String id;
  final String name;
  final String nameRu;
  final int cidrCount;
  final List<String> processes;

  /// English name by default, Russian when the UI is ru.
  String displayName(String langCode) =>
      langCode == 'ru' && nameRu.isNotEmpty ? nameRu : name;

  factory GameInfo.fromJson(Map<String, dynamic> json) => GameInfo(
        id: json['id']?.toString() ?? '',
        name: json['name']?.toString() ?? '',
        nameRu: json['name_ru']?.toString() ?? '',
        cidrCount: (json['cidr_count'] as num?)?.toInt() ?? 0,
        processes: (json['processes'] as List?)?.map((e) => e.toString()).toList() ?? const [],
      );
}

/// Offline snapshot of the catalog, shown when `GET /api/games` fails or
/// returns nothing. Снапшот, сервер отдаёт актуальное (обновление раз в
/// 24ч, docs/dns-games.md §2) — подсети здесь укорочены до нескольких
/// характерных блоков, чтобы экран оставался рабочим офлайн.
const kFallbackGames = <GameInfo>[
  GameInfo(id: 'cs2', name: 'CS2', nameRu: 'CS2', cidrCount: 47, processes: ['cs2.exe', 'cs2']),
  GameInfo(id: 'dota2', name: 'Dota 2', nameRu: 'Dota 2', cidrCount: 47, processes: ['dota2.exe', 'dota2']),
  GameInfo(id: 'valorant', name: 'Valorant', nameRu: 'Valorant', cidrCount: 15, processes: ['VALORANT.exe', 'RiotClientServices.exe']),
  GameInfo(id: 'lol', name: 'League of Legends', nameRu: 'League of Legends', cidrCount: 15, processes: ['LeagueClient.exe']),
  GameInfo(id: 'apex', name: 'Apex Legends', nameRu: 'Apex Legends', cidrCount: 3, processes: ['r5apex.exe']),
  GameInfo(id: 'blizzard', name: 'Blizzard / Overwatch 2', nameRu: 'Blizzard / Overwatch 2', cidrCount: 6, processes: ['Overwatch.exe', 'Battle.net.exe']),
  GameInfo(id: 'pubg', name: 'PUBG: Battlegrounds', nameRu: 'PUBG: Battlegrounds', cidrCount: 3, processes: ['TslGame.exe']),
  GameInfo(id: 'r6', name: 'Rainbow Six Siege', nameRu: 'Rainbow Six Siege', cidrCount: 3, processes: ['RainbowSix.exe']),
  GameInfo(id: 'rust', name: 'Rust', nameRu: 'Rust', cidrCount: 3, processes: ['RustClient.exe']),
  GameInfo(id: 'standoff2', name: 'Standoff 2', nameRu: 'Standoff 2', cidrCount: 3, processes: ['com.axlebolt.standoff2']),
];

/// Snapshot routes per game id.
const _kFallbackCidrs = <String, List<String>>{
  'cs2': [
    '155.133.224.0/22',
    '155.133.228.0/22',
    '146.66.152.0/21',
    '185.25.180.0/22',
    '103.10.124.0/23',
  ],
  'dota2': [
    '146.66.155.0/24',
    '146.66.156.0/22',
    '155.133.236.0/22',
    '155.133.240.0/22',
    '45.121.184.0/22',
  ],
  'valorant': [
    '162.249.72.0/21',
    '64.7.194.0/24',
    '103.240.192.0/22',
    '162.249.80.0/22',
  ],
  'lol': [
    '162.249.72.0/22',
    '64.7.194.0/24',
    '103.240.192.0/22',
    '192.64.174.0/24',
  ],
  'apex': [
    '159.153.0.0/16',
    '178.162.192.0/19',
    '185.179.200.0/22',
  ],
  'blizzard': [
    '24.105.0.0/18',
    '37.244.0.0/18',
    '137.221.0.0/16',
    '185.60.112.0/22',
  ],
  'pubg': [
    '18.194.0.0/15',
    '35.156.0.0/15',
    '52.57.0.0/16',
  ],
  'r6': [
    '162.247.16.0/22',
    '185.38.20.0/22',
    '216.98.48.0/20',
  ],
  'rust': [
    '104.238.128.0/20',
    '185.38.148.0/22',
    '192.223.24.0/22',
  ],
  'standoff2': [
    '92.38.128.0/17',
    '92.223.0.0/16',
    '188.166.0.0/18',
  ],
};

/// Subnets of [gameId] from the offline snapshot; empty when unknown —
/// callers then drop the game from the route union.
List<String> fallbackCidrs(String gameId) => _kFallbackCidrs[gameId] ?? const [];
