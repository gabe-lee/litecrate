clear
echo "+-------------+"
echo "|   TESTING   |"
echo "+-------------+"
go test -coverprofile cover.out 
go tool cover -html=cover.out -o=cover.html
echo "+----------------+"
echo "|   BENCHMARKS   |"
echo "+----------------+"
go test -bench=.
echo "+-------------+"
echo "|   FUZZING   |"
echo "+-------------+"
echo "--- FuzzZigZag"
go test -fuzz=FuzzZigZag -fuzztime 5s -cover
echo "--- FuzzBool"
go test -fuzz=FuzzBool -fuzztime 1s -cover
echo "--- FuzzU8"
go test -fuzz=FuzzU8 -fuzztime 5s -cover
echo "--- FuzzI8"
go test -fuzz=FuzzI8 -fuzztime 5s -cover
echo "--- FuzzU16"
go test -fuzz=FuzzU16 -fuzztime 5s -cover
echo "--- FuzzI16"
go test -fuzz=FuzzI16 -fuzztime 5s -cover
echo "--- FuzzU24"
go test -fuzz=FuzzU24 -fuzztime 5s -cover
echo "--- FuzzI24"
go test -fuzz=FuzzI24 -fuzztime 5s -cover
echo "--- FuzzU32"
go test -fuzz=FuzzU32 -fuzztime 5s -cover
echo "--- FuzzI32"
go test -fuzz=FuzzI32 -fuzztime 5s -cover
echo "--- FuzzU40"
go test -fuzz=FuzzU40 -fuzztime 5s -cover
echo "--- FuzzI40"
go test -fuzz=FuzzI40 -fuzztime 5s -cover
echo "--- FuzzU48"
go test -fuzz=FuzzU48 -fuzztime 5s -cover
echo "--- FuzzI48"
go test -fuzz=FuzzI48 -fuzztime 5s -cover
echo "--- FuzzU56"
go test -fuzz=FuzzU56 -fuzztime 5s -cover
echo "--- FuzzI56"
go test -fuzz=FuzzI56 -fuzztime 5s -cover
echo "--- FuzzU64"
go test -fuzz=FuzzU64 -fuzztime 5s -cover
echo "--- FuzzI64"
go test -fuzz=FuzzI64 -fuzztime 5s -cover
echo "--- FuzzInt"
go test -fuzz=FuzzInt -fuzztime 5s -cover
echo "--- FuzzUint"
go test -fuzz=FuzzUint -fuzztime 5s -cover
echo "--- FuzzUintPtr"
go test -fuzz=FuzzUINTPtr -fuzztime 5s -cover
echo "--- FuzzF32"
go test -fuzz=FuzzF32 -fuzztime 5s -cover
echo "--- FuzzF64"
go test -fuzz=FuzzF64 -fuzztime 5s -cover
echo "--- FuzzC64"
go test -fuzz=FuzzC64 -fuzztime 5s -cover
echo "--- FuzzC128"
go test -fuzz=FuzzC128 -fuzztime 5s -cover
echo "--- FuzzUVarint"
go test -fuzz=FuzzUVarint -fuzztime 5s -cover
echo "--- FuzzVarint"
go test -fuzz=FuzzVarint -fuzztime 5s -cover
echo "--- FuzzLengthOrNil"
go test -fuzz=FuzzLengthOrNil -fuzztime 20s -cover
echo "--- FuzzString"
go test -fuzz=FuzzString -fuzztime 30s -cover
echo "--- FuzzBytes"
go test -fuzz=FuzzBytes -fuzztime 20s -cover
echo "--- FuzzSelfAccessor"
go test -fuzz=FuzzSelfAccessor -fuzztime 5m -cover
