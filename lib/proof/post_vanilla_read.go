package proof

import (
	"context"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/go-state-types/abi"
)

func GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	/*
	   let tree = &replica
	       .merkle_tree(post_config.sector_size)
	       .with_context(|| {
	           format!(
	               "generate_single_vanilla_proof: merkle_tree failed: {:?}",
	               sector_id
	           )
	       })?;
	   let comm_r = replica.safe_comm_r().with_context(|| {
	       format!(
	           "generate_single_vanilla_poof: safe_comm_r failed: {:?}",
	           sector_id
	       )
	   })?;

	*/

	/*
	   let priv_sectors = vec![fallback::PrivateSector {
	       tree,
	       comm_c,
	       comm_r_last,
	   }];
	*/

	/*
		let vanilla_proof =
		        fallback::vanilla_proof(sector_id, &priv_inputs, challenges).with_context(|| {
		            format!(
		                "generate_single_vanilla_proof: vanilla_proof failed: {:?}",
		                sector_id
		            )
		        })?;
	*/

	/// ...>>

	/*
				pub fn vanilla_proof<Tree: MerkleTreeTrait>(
				    sector_id: SectorId,
				    priv_inputs: &PrivateInputs<'_, Tree>,
				    challenges: &[u64],
				) -> Result<Proof<Tree::Proof>> {


			    let tree_leafs = tree.leafs();
			    let rows_to_discard = default_rows_to_discard(tree_leafs, Tree::Arity::to_usize());

		let inclusion_proofs = (0..challenges.len())
		        .into_par_iter()
		        .map(|challenged_leaf_index| {
		            let challenged_leaf = challenges[challenged_leaf_index];
		            let proof = tree.gen_cached_proof(challenged_leaf as usize, Some(rows_to_discard))?;

		            ensure!(
		                proof.validate(challenged_leaf as usize) && proof.root() == priv_sector.comm_r_last,
		                "Generated vanilla proof for sector {} is invalid",
		                sector_id
		            );

		            Ok(proof)
		        })
		        .collect::<Result<Vec<_>>>()?;
	*/

	/// ..>>

	/*
		fn gen_cached_proof(&self, i: usize, rows_to_discard: Option<usize>) -> Result<Self::Proof> {
		        if rows_to_discard.is_some() && rows_to_discard.expect("rows to discard failure") == 0 {
		            return self.gen_proof(i);
		        }

		        let proof = self.inner.gen_cached_proof(i, rows_to_discard)?;

		        debug_assert!(proof.validate::<H::Function>().expect("validate failed"));

		        MerkleProof::try_from_proof(proof)
		    }
	*/

	// gen_cached_proof

	/*

	 */

	/*
		#[derive(Clone, Eq, PartialEq)]
		#[allow(clippy::enum_variant_names)]
		enum Data<E: Element, A: Algorithm<E>, S: Store<E>, BaseTreeArity: Unsigned, SubTreeArity: Unsigned>
		{
		    /// A BaseTree contains a single Store.
		    BaseTree(S),

		    /// A SubTree contains a list of BaseTrees.
		    SubTree(Vec<MerkleTree<E, A, S, BaseTreeArity>>),

		    /// A TopTree contains a list of SubTrees.
		    TopTree(Vec<MerkleTree<E, A, S, BaseTreeArity, SubTreeArity>>),
		}
	*/

	panic("aoe")
}
