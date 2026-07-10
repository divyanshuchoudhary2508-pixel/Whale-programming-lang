#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <stdint.h>
#include <ctype.h>
#include <pthread.h>

// Whale types
typedef struct {
    int64_t len;
    void* ptr;
} WhaleArray;

// --- Garbage Collector ---
typedef struct Allocation {
    void* ptr;
    size_t size;
    int marked;
    struct Allocation* next;
} Allocation;

Allocation* whale_alloc_head = NULL;
void* whale_stack_top = NULL;
size_t whale_allocated_bytes = 0;
size_t whale_gc_threshold = 1024 * 1024; // 1MB

pthread_mutex_t whale_gc_mutex = PTHREAD_MUTEX_INITIALIZER;

void whale_gc_init(void* stack_top) {
    whale_stack_top = stack_top;
}

// Simple recursive marking (naive)
void gc_mark(void* ptr) {
    for (Allocation* a = whale_alloc_head; a != NULL; a = a->next) {
        if (a->ptr == ptr && !a->marked) {
            a->marked = 1;
            // recursively scan inside this struct for more pointers
            void** block_start = (void**)a->ptr;
            void** block_end = (void**)((char*)a->ptr + a->size);
            for (void** bp = block_start; bp < block_end; bp++) {
                gc_mark(*bp);
            }
            return;
        }
    }
}

void whale_gc_run() {
    pthread_mutex_lock(&whale_gc_mutex);
    
    for (Allocation* a = whale_alloc_head; a != NULL; a = a->next) {
        a->marked = 0;
    }
    
    void* stack_bottom = &stack_bottom; 
    void** start = (void**)(stack_bottom < whale_stack_top ? stack_bottom : whale_stack_top);
    void** end = (void**)(stack_bottom > whale_stack_top ? stack_bottom : whale_stack_top);
    
    for (void** p = start; p < end; p++) {
        gc_mark(*p);
    }
    
    Allocation** curr = &whale_alloc_head;
    while (*curr != NULL) {
        Allocation* entry = *curr;
        if (!entry->marked) {
            *curr = entry->next;
            whale_allocated_bytes -= entry->size;
            free(entry->ptr);
            free(entry);
        } else {
            curr = &entry->next;
        }
    }
    
    whale_gc_threshold = whale_allocated_bytes * 2;
    if (whale_gc_threshold < 1024 * 1024) whale_gc_threshold = 1024 * 1024;
    
    pthread_mutex_unlock(&whale_gc_mutex);
}

void* whale_malloc(size_t size) {
    if (whale_allocated_bytes + size > whale_gc_threshold && whale_stack_top != NULL) {
        whale_gc_run();
    }
    
    void* ptr = calloc(1, size); // calloc clears memory to 0
    if (!ptr) return NULL;
    
    pthread_mutex_lock(&whale_gc_mutex);
    Allocation* a = malloc(sizeof(Allocation));
    a->ptr = ptr;
    a->size = size;
    a->marked = 0;
    a->next = whale_alloc_head;
    whale_alloc_head = a;
    whale_allocated_bytes += size;
    pthread_mutex_unlock(&whale_gc_mutex);
    
    return ptr;
}


// String Standard Library
char* whale_read_file(char* filename) {
    FILE* f = fopen(filename, "rb");
    if (!f) return strdup("");
    fseek(f, 0, SEEK_END);
    long fsize = ftell(f);
    fseek(f, 0, SEEK_SET);

    char* str = whale_malloc(fsize + 1);
    fread(str, 1, fsize, f);
    fclose(f);
    str[fsize] = 0;
    return str;
}

void whale_write_file(char* filename, char* data) {
    FILE* f = fopen(filename, "wb");
    if (!f) return;
    fputs(data, f);
    fclose(f);
}

WhaleArray whale_lines(char* str) {
    // Count lines
    int count = 1;
    for (int i = 0; str[i]; i++) {
        if (str[i] == '\n') count++;
    }

    char** arr = whale_malloc(sizeof(char*) * count);
    char* copy = strdup(str);
    char* token = strtok(copy, "\n");
    int i = 0;
    while (token) {
        arr[i++] = strdup(token);
        token = strtok(NULL, "\n");
    }
    free(copy);

    WhaleArray res;
    res.len = i;
    res.ptr = arr;
    return res;
}

WhaleArray whale_split(char* str, char* delim) {
    // simplistic split
    int count = 1;
    char* p = str;
    int dlen = strlen(delim);
    if (dlen == 0) dlen = 1; // avoid infinite loop
    while ((p = strstr(p, delim))) {
        count++;
        p += dlen;
    }

    char** arr = whale_malloc(sizeof(char*) * count);
    char* copy = strdup(str);
    int i = 0;
    char* token = strtok(copy, delim);
    while (token) {
        arr[i++] = strdup(token);
        token = strtok(NULL, delim);
    }
    free(copy);

    WhaleArray res;
    res.len = i;
    res.ptr = arr;
    return res;
}

char* whale_trim(char* str) {
    while(isspace(*str)) str++;
    if(*str == 0) return strdup("");
    char* end = str + strlen(str) - 1;
    while(end > str && isspace(*end)) end--;
    end[1] = '\0';
    return strdup(str);
}

char* whale_to_lower(char* str) {
    char* res = strdup(str);
    for (int i = 0; res[i]; i++) res[i] = tolower(res[i]);
    return res;
}

char* whale_to_upper(char* str) {
    char* res = strdup(str);
    for (int i = 0; res[i]; i++) res[i] = toupper(res[i]);
    return res;
}

char* whale_replace(char* str, char* oldStr, char* newStr) {
    // simplified: just return a copy for now to prevent crashing
    return strdup(str);
}

// Math Standard Library
double whale_abs_f(double x) { return fabs(x); }
int64_t whale_abs_i(int64_t x) { return x < 0 ? -x : x; }

double whale_max_f(double a, double b) { return a > b ? a : b; }
int64_t whale_max_i(int64_t a, int64_t b) { return a > b ? a : b; }

double whale_min_f(double a, double b) { return a < b ? a : b; }
int64_t whale_min_i(int64_t a, int64_t b) { return a < b ? a : b; }

int64_t whale_parse_int(char* str) {
    return strtoll(str, NULL, 10);
}

// Array operations
WhaleArray whale_push(WhaleArray arr, int64_t elem) {
    // For simplicity, reallocate. We'd ideally need the element size.
    // Assuming int64_t (8 bytes) for MVP
    int64_t* new_ptr = whale_malloc(8 * (arr.len + 1));
    if (arr.len > 0) {
        memcpy(new_ptr, arr.ptr, 8 * arr.len);
    }
    new_ptr[arr.len] = elem;
    
    WhaleArray res;
    res.len = arr.len + 1;
    res.ptr = new_ptr;
    return res;
}

WhaleArray whale_pop(WhaleArray arr) {
    if (arr.len == 0) return arr;
    WhaleArray res;
    res.len = arr.len - 1;
    res.ptr = arr.ptr;
    return res;
}

// ----------------------------------------------------------------------------
// Concurrency (Channels and Spawn)
// ----------------------------------------------------------------------------

typedef struct {
    int64_t* buffer;
    int capacity;
    int head;
    int tail;
    int count;
    pthread_mutex_t mutex;
    pthread_cond_t not_empty;
    pthread_cond_t not_full;
} WhaleChan;

void* whale_chan_new() {
    WhaleChan* ch = whale_malloc(sizeof(WhaleChan));
    ch->capacity = 16; // buffered channel for simplicity
    ch->buffer = whale_malloc(sizeof(int64_t) * ch->capacity);
    ch->head = 0; ch->tail = 0; ch->count = 0;
    pthread_mutex_init(&ch->mutex, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    return ch;
}

void whale_chan_send(void* ch_ptr, int64_t val) {
    WhaleChan* ch = (WhaleChan*)ch_ptr;
    pthread_mutex_lock(&ch->mutex);
    while (ch->count == ch->capacity) {
        pthread_cond_wait(&ch->not_full, &ch->mutex);
    }
    ch->buffer[ch->tail] = val;
    ch->tail = (ch->tail + 1) % ch->capacity;
    ch->count++;
    pthread_cond_signal(&ch->not_empty);
    pthread_mutex_unlock(&ch->mutex);
}

int64_t whale_chan_recv(void* ch_ptr) {
    WhaleChan* ch = (WhaleChan*)ch_ptr;
    pthread_mutex_lock(&ch->mutex);
    while (ch->count == 0) {
        pthread_cond_wait(&ch->not_empty, &ch->mutex);
    }
    int64_t val = ch->buffer[ch->head];
    ch->head = (ch->head + 1) % ch->capacity;
    ch->count--;
    pthread_cond_signal(&ch->not_full);
    pthread_mutex_unlock(&ch->mutex);
    return val;
}

void whale_spawn_worker(void* (*func)(void*), void* arg) {
    pthread_t thread;
    pthread_create(&thread, NULL, func, arg);
    pthread_detach(thread);
}

// ----------------------------------------------------------------------------
// Map (Native Bindings)
// ----------------------------------------------------------------------------

typedef struct MapNode {
    char* key;
    char* value;
    struct MapNode* next;
} MapNode;

typedef struct {
    MapNode** buckets;
    int capacity;
} WhaleMap;

int64_t whale_map_new() {
    WhaleMap* m = whale_malloc(sizeof(WhaleMap));
    m->capacity = 16;
    m->buckets = whale_malloc(sizeof(MapNode*) * m->capacity);
    return (int64_t)m;
}

uint32_t hash_str(char* str) {
    uint32_t hash = 2166136261u;
    for (int i = 0; str[i]; i++) {
        hash ^= (uint8_t)str[i];
        hash *= 16777619;
    }
    return hash;
}

void whale_map_set(int64_t map_ptr, char* key, char* value) {
    WhaleMap* m = (WhaleMap*)map_ptr;
    uint32_t idx = hash_str(key) % m->capacity;
    MapNode* curr = m->buckets[idx];
    while (curr) {
        if (strcmp(curr->key, key) == 0) {
            curr->value = strdup(value);
            return;
        }
        curr = curr->next;
    }
    MapNode* n = whale_malloc(sizeof(MapNode));
    n->key = strdup(key);
    n->value = strdup(value);
    n->next = m->buckets[idx];
    m->buckets[idx] = n;
}

char* whale_map_get(int64_t map_ptr, char* key) {
    WhaleMap* m = (WhaleMap*)map_ptr;
    uint32_t idx = hash_str(key) % m->capacity;
    MapNode* curr = m->buckets[idx];
    while (curr) {
        if (strcmp(curr->key, key) == 0) {
            return curr->value;
        }
        curr = curr->next;
    }
    return strdup("");
}

// ----------------------------------------------------------------------------
// Networking (Native Bindings)
// ----------------------------------------------------------------------------

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#else
#include <sys/socket.h>
#include <netinet/in.h>
#include <unistd.h>
#include <arpa/inet.h>
#endif

int net_initialized = 0;

void ensure_net_init() {
    if (!net_initialized) {
        net_initialized = 1;
#ifdef _WIN32
        WSADATA wsaData;
        WSAStartup(MAKEWORD(2, 2), &wsaData);
#endif
    }
}

int64_t whale_net_listen(int64_t port) {
    ensure_net_init();
    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    struct sockaddr_in address;
    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, (char*)&opt, sizeof(opt));
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = INADDR_ANY;
    address.sin_port = htons(port);
    bind(server_fd, (struct sockaddr*)&address, sizeof(address));
    listen(server_fd, 3);
    return server_fd;
}

int64_t whale_net_accept(int64_t server_fd) {
    struct sockaddr_in address;
    int addrlen = sizeof(address);
    int new_socket = accept(server_fd, (struct sockaddr*)&address, (socklen_t*)&addrlen);
    return new_socket;
}

char* whale_net_recv(int64_t fd) {
    char* buffer = whale_malloc(4096);
    int valread = recv(fd, buffer, 4095, 0);
    if (valread <= 0) return strdup("");
    buffer[valread] = '\0';
    return buffer;
}

void whale_net_send(int64_t fd, char* data) {
    send(fd, data, strlen(data), 0);
}

void whale_net_close(int64_t fd) {
#ifdef _WIN32
    closesocket(fd);
#else
    close(fd);
#endif
}

int64_t whale_net_dial(char* host, int64_t port) {
    ensure_net_init();
    int sock = socket(AF_INET, SOCK_STREAM, 0);
    struct sockaddr_in serv_addr;
    serv_addr.sin_family = AF_INET;
    serv_addr.sin_port = htons(port);
    inet_pton(AF_INET, host, &serv_addr.sin_addr);
    connect(sock, (struct sockaddr*)&serv_addr, sizeof(serv_addr));
    return sock;
}
