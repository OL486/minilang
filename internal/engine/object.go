package engine

// ObjType 只用于调试输出与 GC 统计。VM 分派靠 Go 的类型断言，
// 不靠这个标签 —— 两者并存是故意的：标签便宜，方便 --stats 打对象分布。
type ObjType uint8

const (
	ObjStringType ObjType = iota
	ObjFunctionType
	ObjClosureType
	ObjUpvalueType
	ObjNativeType
)

func (t ObjType) String() string {
	switch t {
	case ObjStringType:
		return "string"
	case ObjFunctionType:
		return "function"
	case ObjClosureType:
		return "closure"
	case ObjUpvalueType:
		return "upvalue"
	case ObjNativeType:
		return "native"
	}
	return "unknown"
}

// objHeader 是所有堆对象的公共头。
//
// next 构成「所有对象」的单向链表 —— 这是 mark-sweep 的遍历基础。
// 没有它就没法在 GC 时枚举对象，只能靠 Go 的 GC 反射，那就不是自己实现了。
//
// self 是回指具体对象自身的指针。看起来多余，实际是必需的：
// GC 拿到的是 *objHeader，而释放一个字符串时要把它从驻留表里删掉，
// 这需要 *ObjString。没有 self 就只能用 unsafe.Pointer 强转。
type objHeader struct {
	typ    ObjType
	marked bool
	next   *objHeader
	self   Object
	size   int // 估算字节数，供 GC 统计与触发阈值使用
}

// Object 是所有堆对象的接口。
//
// header 必须容忍 nil 接收者：GC 标记时可能遇到「还没填好的槽位」。
// 例如 OP_CLOSURE 先把闭包压栈再逐个填 upvalue，
// 填第一个 upvalue 时如果触发 GC，闭包的 Upvalues[1..] 还是 nil。
// 一个 nil 的 *ObjUpvalue 装进 Object 接口后并不等于 nil 接口，
// 这时候如果直接解引用就 panic —— 这个坑只在「恰好那一次分配触发 GC」时出现。
type Object interface {
	header() *objHeader
}

// ObjString 是字符串对象。Chars 用 Go string 而不是 []byte：
// minilang 的字符串不可变、不支持下标访问，用 Go string 可以让
// 「拼接产生新对象」这件事天然成立，也省掉一堆 copy。
type ObjString struct {
	objHeader
	Chars string
	Hash  uint32
}

func (s *ObjString) header() *objHeader {
	if s == nil {
		return nil
	}
	return &s.objHeader
}

// ObjFunction 是「编译产物」：一段字节码 + 元信息。
// 它不含运行期状态，所以同一个函数的所有闭包共享一个 ObjFunction。
type ObjFunction struct {
	objHeader
	Name         string
	Arity        int
	UpvalueCount int
	Chunk        *Chunk
}

func (f *ObjFunction) header() *objHeader {
	if f == nil {
		return nil
	}
	return &f.objHeader
}

// ObjUpvalue 是「捕获的变量」。
//
// 两种状态：
//   - open：Location 指向 VM 栈上的某个槽位，读写直接作用在栈上；
//   - closed：变量所在作用域已退出，Closed 字段保存值的副本，Location 指向 Closed。
//
// 为什么不能一开始就拷贝：闭包捕获的变量必须和原变量共享同一块存储，
// 否则 makeCounter 里的 count 会变成两份，两个计数器就各自加各自的。
//
// Slot 是 open 状态下对应的栈槽位下标。为什么不用指针大小做排序：
// Go 不允许对指针做 < > 比较（只有 == !=），所以「按栈槽降序排列」这件事
// 必须靠一个整数来表达。Slot 只在 open 状态有效。
type ObjUpvalue struct {
	objHeader
	Location *Value
	Slot     int
	Closed   Value
	Next     *ObjUpvalue // open upvalue 链表，按 Slot 降序
}

func (u *ObjUpvalue) header() *objHeader {
	if u == nil {
		return nil
	}
	return &u.objHeader
}

// ObjClosure 是运行期的函数值：ObjFunction + 一组 upvalue。
// 同一段代码 + 不同环境 = 不同闭包，这是「函数是一等公民」的实现基础。
type ObjClosure struct {
	objHeader
	Fn       *ObjFunction
	Upvalues []*ObjUpvalue
}

func (c *ObjClosure) header() *objHeader {
	if c == nil {
		return nil
	}
	return &c.objHeader
}

// ObjNative 是内置函数。Fn 是 Go 闭包，绕开字节码直接执行。
type ObjNative struct {
	objHeader
	Name  string
	Arity int
	Fn    func(vm *VM, args []Value) (Value, error)
}

func (n *ObjNative) header() *objHeader {
	if n == nil {
		return nil
	}
	return &n.objHeader
}

// hashString 是 FNV-1a。用于驻留表的桶下标。
func hashString(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}
