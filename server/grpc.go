package server

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/srjn45/filedbv2/internal/engine"
	pb "github.com/srjn45/filedbv2/internal/pb/proto"
	"github.com/srjn45/filedbv2/internal/query"
	"github.com/srjn45/filedbv2/internal/store"
)

// GRPCServer implements pb.FileDBServer.
type GRPCServer struct {
	pb.UnimplementedFileDBServer
	db *engine.DB
}

// NewGRPCServer creates a GRPCServer backed by the given DB.
func NewGRPCServer(db *engine.DB) *GRPCServer {
	return &GRPCServer{db: db}
}

// ---- Collection management ------------------------------------------------

func (s *GRPCServer) CreateCollection(_ context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name required")
	}
	if _, err := s.db.CreateCollection(req.Name); err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "%v", err)
	}
	return &pb.CreateCollectionResponse{
		Name:      req.Name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *GRPCServer) DropCollection(_ context.Context, req *pb.DropCollectionRequest) (*pb.DropCollectionResponse, error) {
	if err := s.db.DropCollection(req.Name); err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.DropCollectionResponse{Ok: true}, nil
}

func (s *GRPCServer) ListCollections(_ context.Context, _ *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	return &pb.ListCollectionsResponse{Names: s.db.ListCollections()}, nil
}

// ---- CRUD -----------------------------------------------------------------

func (s *GRPCServer) Insert(_ context.Context, req *pb.InsertRequest) (*pb.InsertResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	data := req.Data.AsMap()
	id, ts, err := col.Insert(data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "insert: %v", err)
	}
	return &pb.InsertResponse{Id: id, DateAdded: ts.Format(time.RFC3339)}, nil
}

func (s *GRPCServer) InsertMany(_ context.Context, req *pb.InsertManyRequest) (*pb.InsertManyResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	ids := make([]uint64, 0, len(req.Records))
	for _, r := range req.Records {
		id, _, err := col.Insert(r.AsMap())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "insertMany: %v", err)
		}
		ids = append(ids, id)
	}
	return &pb.InsertManyResponse{Ids: ids}, nil
}

func (s *GRPCServer) FindById(_ context.Context, req *pb.FindByIdRequest) (*pb.FindResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "collection: %v", err)
	}
	data, ts, err := col.FindByID(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	rec, err := toProtoRecord(req.Id, data, ts)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &pb.FindResponse{Record: rec}, nil
}

func (s *GRPCServer) Find(req *pb.FindRequest, stream pb.FileDB_FindServer) error {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return status.Errorf(codes.NotFound, "%v", err)
	}

	f, err := protoFilterToQuery(req.Filter)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "filter: %v", err)
	}

	results, err := col.Scan(f)
	if err != nil {
		return status.Errorf(codes.Internal, "scan: %v", err)
	}

	// Apply offset and limit.
	start := int(req.Offset)
	if start > len(results) {
		start = len(results)
	}
	results = results[start:]
	if req.Limit > 0 && int(req.Limit) < len(results) {
		results = results[:req.Limit]
	}

	for _, r := range results {
		rec, err := toProtoRecord(r.ID, r.Data, r.Ts)
		if err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
		if err := stream.Send(&pb.FindResponse{Record: rec}); err != nil {
			return err
		}
	}
	return nil
}

func (s *GRPCServer) Update(_ context.Context, req *pb.UpdateRequest) (*pb.UpdateResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	ts, err := col.Update(req.Id, req.Data.AsMap())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.UpdateResponse{Id: req.Id, DateModified: ts.Format(time.RFC3339)}, nil
}

func (s *GRPCServer) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	if err := col.Delete(req.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.DeleteResponse{Ok: true}, nil
}

// ---- Watch ----------------------------------------------------------------

func (s *GRPCServer) Watch(req *pb.WatchRequest, stream pb.FileDB_WatchServer) error {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return status.Errorf(codes.NotFound, "%v", err)
	}

	f, err := protoFilterToQuery(req.Filter)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "filter: %v", err)
	}

	_, ch, cancel := col.Subscribe()
	defer cancel()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if !f.Match(ev.Data) {
				continue
			}
			var op pb.WatchOp
			switch ev.Op {
			case store.OpInsert:
				op = pb.WatchOp_INSERTED
			case store.OpUpdate:
				op = pb.WatchOp_UPDATED
			case store.OpDelete:
				op = pb.WatchOp_DELETED
			}
			rec, _ := toProtoRecord(ev.ID, ev.Data, ev.Ts)
			if err := stream.Send(&pb.WatchEvent{
				Op:         op,
				Collection: req.Collection,
				Record:     rec,
				Ts:         timestamppb.New(ev.Ts),
			}); err != nil {
				return err
			}
		}
	}
}

// ---- Stats ----------------------------------------------------------------

func (s *GRPCServer) CollectionStats(_ context.Context, req *pb.CollectionStatsRequest) (*pb.CollectionStatsResponse, error) {
	col, err := s.db.Collection(req.Collection)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	st := col.Stats()
	return &pb.CollectionStatsResponse{
		Collection:   st.Name,
		RecordCount:  st.RecordCount,
		SegmentCount: st.SegmentCount,
		DirtyEntries: st.DirtyEntries,
		SizeBytes:    st.SizeBytes,
	}, nil
}

// ---- Transactions (stub) --------------------------------------------------

func (s *GRPCServer) BeginTx(_ context.Context, _ *pb.BeginTxRequest) (*pb.BeginTxResponse, error) {
	return nil, status.Error(codes.Unimplemented, "transactions not yet implemented")
}
func (s *GRPCServer) CommitTx(_ context.Context, _ *pb.CommitTxRequest) (*pb.CommitTxResponse, error) {
	return nil, status.Error(codes.Unimplemented, "transactions not yet implemented")
}
func (s *GRPCServer) RollbackTx(_ context.Context, _ *pb.RollbackTxRequest) (*pb.RollbackTxResponse, error) {
	return nil, status.Error(codes.Unimplemented, "transactions not yet implemented")
}

// ---- Helpers --------------------------------------------------------------

func toProtoRecord(id uint64, data map[string]any, ts time.Time) (*pb.Record, error) {
	s, err := structpb.NewStruct(data)
	if err != nil {
		return nil, fmt.Errorf("toProtoRecord: %w", err)
	}
	return &pb.Record{
		Id:           id,
		Data:         s,
		DateAdded:    timestamppb.New(ts),
		DateModified: timestamppb.New(ts),
	}, nil
}

func protoFilterToQuery(f *pb.Filter) (query.Filter, error) {
	if f == nil {
		return query.MatchAll, nil
	}
	switch k := f.Kind.(type) {
	case *pb.Filter_Field:
		return &query.FieldFilter{
			Field: k.Field.Field,
			Op:    protoOpToQuery(k.Field.Op),
			Value: k.Field.Value,
		}, nil
	case *pb.Filter_And:
		sub := make([]query.Filter, 0, len(k.And.Filters))
		for _, ff := range k.And.Filters {
			qf, err := protoFilterToQuery(ff)
			if err != nil {
				return nil, err
			}
			sub = append(sub, qf)
		}
		return &query.AndFilter{Filters: sub}, nil
	case *pb.Filter_Or:
		sub := make([]query.Filter, 0, len(k.Or.Filters))
		for _, ff := range k.Or.Filters {
			qf, err := protoFilterToQuery(ff)
			if err != nil {
				return nil, err
			}
			sub = append(sub, qf)
		}
		return &query.OrFilter{Filters: sub}, nil
	}
	return query.MatchAll, nil
}

func protoOpToQuery(op pb.FilterOp) query.Op {
	switch op {
	case pb.FilterOp_EQ:
		return query.OpEq
	case pb.FilterOp_NEQ:
		return query.OpNeq
	case pb.FilterOp_GT:
		return query.OpGt
	case pb.FilterOp_GTE:
		return query.OpGte
	case pb.FilterOp_LT:
		return query.OpLt
	case pb.FilterOp_LTE:
		return query.OpLte
	case pb.FilterOp_CONTAINS:
		return query.OpContains
	case pb.FilterOp_REGEX:
		return query.OpRegex
	default:
		return query.OpEq
	}
}
